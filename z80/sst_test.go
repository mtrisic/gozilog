package z80

// Harness for the SingleStepTests Z80 suite
// (https://github.com/SingleStepTests/z80): per-opcode JSON files with
// 1000 randomized cases each — initial state, expected final state, and
// per-T-state bus activity. The data (~1.5 GB unpacked) is downloaded by
// .devcontainer/post_create.sh into testdata/sst/v1 and never committed.
//
// Every case is asserted at state level (all registers incl. WZ/Q/P,
// RAM, port transactions), on total T-state count, and — since Phase 3
// — on the full per-T-state cycle trace (address bus, data bus, and
// RD/WR/MREQ/IORQ pins at every T-state).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sstDir = "testdata/sst/v1"

type sstCase struct {
	Name    string   `json:"name"`
	Initial sstState `json:"initial"`
	Final   sstState `json:"final"`
	Cycles  [][3]any `json:"cycles"`
	// Ports lists I/O traffic as [port, value, "r"|"w"] triples, in
	// order: reads supply the value the bus must return, writes assert
	// the value the CPU must emit.
	Ports [][3]any `json:"ports"`
}

type sstState struct {
	PC, SP                 uint16
	A, B, C, D, E, F, H, L byte
	I, R                   byte
	EI                     byte
	WZ, IX, IY             uint16
	AF2                    uint16 `json:"af_"`
	BC2                    uint16 `json:"bc_"`
	DE2                    uint16 `json:"de_"`
	HL2                    uint16 `json:"hl_"`
	IM                     byte
	P, Q                   byte
	IFF1, IFF2             byte
	RAM                    [][2]uint16 `json:"ram"`
}

func (s *sstState) toState() State {
	return State{
		AF:  uint16(s.A)<<8 | uint16(s.F),
		BC:  uint16(s.B)<<8 | uint16(s.C),
		DE:  uint16(s.D)<<8 | uint16(s.E),
		HL:  uint16(s.H)<<8 | uint16(s.L),
		AF2: s.AF2, BC2: s.BC2, DE2: s.DE2, HL2: s.HL2,
		IX: s.IX, IY: s.IY, SP: s.SP, PC: s.PC, WZ: s.WZ,
		I: s.I, R: s.R, IM: s.IM,
		IFF1: s.IFF1 != 0, IFF2: s.IFF2 != 0,
		EIPending: s.EI != 0,
		Q:         s.Q, P: s.P,
	}
}

// sstBus is a flat 64K test machine that records every write (so memory
// can be reset cheaply between cases) and, as a Ticker, every T-state.
type sstBus struct {
	mem     [65536]byte
	dirty   []uint16
	cycles  []sstCycle
	record  bool
	ports   [][3]any
	portIdx int
	portErr string
}

type sstCycle struct {
	addr uint16
	data int16
	pins Pins
}

func (b *sstBus) MemRead(addr uint16) byte { return b.mem[addr] }
func (b *sstBus) MemWrite(addr uint16, data byte) {
	b.mem[addr] = data
	b.dirty = append(b.dirty, addr)
}
func (b *sstBus) IORead(port uint16) byte {
	v, err := b.nextPort(port, "r", 0)
	if err != "" {
		b.portErr = err
	}
	return v
}

func (b *sstBus) IOWrite(port uint16, v byte) {
	if _, err := b.nextPort(port, "w", v); err != "" {
		b.portErr = err
	}
}

// nextPort consumes the next expected I/O transaction and validates it.
func (b *sstBus) nextPort(port uint16, dir string, wrote byte) (byte, string) {
	if b.portIdx >= len(b.ports) {
		return 0xFF, fmt.Sprintf("unexpected IO %s on port %04x", dir, port)
	}
	p := b.ports[b.portIdx]
	b.portIdx++
	wantPort, _ := p[0].(float64)
	val, _ := p[1].(float64)
	wantDir, _ := p[2].(string)
	if uint16(wantPort) != port || wantDir != dir {
		return byte(val), fmt.Sprintf("IO %s on port %04x, want %s on %04x", dir, port, wantDir, uint16(wantPort))
	}
	if dir == "w" && wrote != byte(val) {
		return 0, fmt.Sprintf("IO write %02x to port %04x, want %02x", wrote, port, byte(val))
	}
	return byte(val), ""
}

func (b *sstBus) Tick(addr uint16, data int16, pins Pins) int {
	if b.record {
		b.cycles = append(b.cycles, sstCycle{addr, data, pins})
	}
	return 0
}

func (b *sstBus) set(addr, val uint16) {
	b.mem[addr] = byte(val)
	b.dirty = append(b.dirty, addr)
}

func (b *sstBus) reset() {
	for _, a := range b.dirty {
		b.mem[a] = 0
	}
	b.dirty = b.dirty[:0]
	b.cycles = b.cycles[:0]
	b.ports = nil
	b.portIdx = 0
	b.portErr = ""
}

// fileImplemented reports whether the opcode(s) a test file exercises
// are implemented yet. File names: "3e", "cb 06", "ed a0", "dd 21",
// "dd cb __ 46" (and fd variants).
func fileImplemented(name string) bool {
	parts := strings.Split(name, " ")
	var op byte
	if _, err := fmt.Sscanf(parts[len(parts)-1], "%02x", &op); err != nil {
		return false
	}
	switch {
	case len(parts) == 1:
		return implementedBase[op]
	case parts[0] == "cb":
		return implementedCB
	case parts[0] == "ed":
		return implementedED
	case len(parts) == 2: // "dd xx" / "fd xx"
		return implementedIdx && (implementedBase[op] || op == 0xCB)
	default: // "dd cb __ xx" / "fd cb __ xx"
		return implementedIdx && implementedCB
	}
}

// TestSingleStep runs every SingleStepTests file whose opcodes are
// implemented and reports the not-yet-implemented remainder.
func TestSingleStep(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(sstDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Skipf("SingleStepTests data not present at %s — run .devcontainer/post_create.sh (inside the devcontainer) to download it", sstDir)
	}

	var run int
	var skipped []string
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		if !fileImplemented(name) {
			skipped = append(skipped, name)
			continue
		}
		run++
		path := path
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runSSTFile(t, path)
		})
	}
	if len(skipped) > 40 {
		t.Logf("%d of %d test files run; %d skipped (not yet implemented), e.g. %v ...",
			run, len(files), len(skipped), skipped[:40])
	} else {
		t.Logf("%d of %d test files run; skipped (not yet implemented): %v", run, len(files), skipped)
	}
}

func runSSTFile(t *testing.T, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var cases []sstCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}

	bus := &sstBus{record: true}
	for i := range cases {
		tc := &cases[i]
		bus.reset()
		bus.ports = tc.Ports
		for _, av := range tc.Initial.RAM {
			bus.set(av[0], av[1])
		}
		cpu := New(bus)
		cpu.SetState(tc.Initial.toState())

		used := cpu.Step()

		if bus.portErr != "" {
			t.Fatalf("%s: %s", tc.Name, bus.portErr)
		}
		if bus.portIdx != len(bus.ports) {
			t.Fatalf("%s: %d of %d expected IO transactions performed", tc.Name, bus.portIdx, len(bus.ports))
		}

		got, want := cpu.State(), tc.Final.toState()
		want.Halted = got.Halted // SST state records carry no halted flag
		if got != want {
			t.Fatalf("%s: state mismatch\n got: %+v\nwant: %+v", tc.Name, got, want)
		}
		for _, av := range tc.Final.RAM {
			if got := bus.mem[av[0]]; got != byte(av[1]) {
				t.Fatalf("%s: ram[%d] = %d, want %d", tc.Name, av[0], got, av[1])
			}
		}
		if used != len(tc.Cycles) {
			t.Fatalf("%s: consumed %d T-states, want %d", tc.Name, used, len(tc.Cycles))
		}
		if diff := diffCycles(bus.cycles, tc.Cycles); diff != "" {
			t.Fatalf("%s: cycle trace mismatch: %s", tc.Name, diff)
		}
	}
}

// diffCycles compares a recorded trace against the JSON cycle list.
// SST pin strings are "rwmi" (RD, WR, MREQ, IORQ; '-' = inactive);
// M1/RFSH/HALT are not recorded by the suite and are masked out.
func diffCycles(got []sstCycle, want [][3]any) string {
	if len(got) != len(want) {
		return fmt.Sprintf("length %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		var pins Pins
		if s, ok := w[2].(string); ok {
			for _, ch := range s {
				switch ch {
				case 'r':
					pins |= RD
				case 'w':
					pins |= WR
				case 'm':
					pins |= MREQ
				case 'i':
					pins |= IORQ
				}
			}
		}
		g := got[i]
		if gp := g.pins &^ (M1 | RFSH | HALTP); gp != pins {
			return fmt.Sprintf("T%d: pins %04b, want %04b", i+1, gp, pins)
		}
		if a, ok := w[0].(float64); ok && g.addr != uint16(a) {
			return fmt.Sprintf("T%d: addr %04x, want %04x", i+1, g.addr, uint16(a))
		}
		wantData := int16(-1)
		if d, ok := w[1].(float64); ok {
			wantData = int16(d)
		}
		if g.data != wantData {
			return fmt.Sprintf("T%d: data %d, want %d", i+1, g.data, wantData)
		}
	}
	return ""
}
