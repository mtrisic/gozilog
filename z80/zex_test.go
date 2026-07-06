package z80

// ZEXDOC/ZEXALL instruction exercisers (Frank D. Cringle; binaries from
// https://github.com/agn453/ZEXALL, downloaded by post_create.sh —
// GPL-licensed test fixtures, never committed). Each runs ~46.7 billion
// T-states through every documented (zexdoc) or all (zexall, incl.
// undocumented X/Y flag) instruction forms and compares CRCs of the
// machine state against values recorded from real hardware.
//
// The programs are CP/M .com files: loaded at 0x0100, they print via
// BDOS calls (CALL 0x0005 with C=2: char in E, C=9: $-terminated
// string at DE) and finish by jumping to 0x0000 (warm boot). The stub
// below traps both addresses in the run loop.

import (
	"os"
	"strings"
	"testing"
)

// zexBus is a plain 64K RAM machine (no Ticker: full emulation speed).
type zexBus struct {
	mem [65536]byte
}

func (b *zexBus) MemRead(addr uint16) byte     { return b.mem[addr] }
func (b *zexBus) MemWrite(addr uint16, v byte) { b.mem[addr] = v }
func (b *zexBus) IORead(port uint16) byte      { return 0xFF }
func (b *zexBus) IOWrite(port uint16, v byte)  {}

func runZex(t *testing.T, path string) {
	program, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s not present — run .devcontainer/post_create.sh (inside the devcontainer) to download it", path)
	}
	if testing.Short() {
		t.Skip("zex exercisers take minutes; skipped with -short")
	}

	bus := &zexBus{}
	copy(bus.mem[0x0100:], program)
	cpu := New(bus)
	cpu.SetState(State{PC: 0x0100, SP: 0xF000})

	var out strings.Builder
	const limit = 60_000_000_000 // ~46.7e9 needed; generous margin
	for {
		// In-package test: direct register access keeps the trap check
		// out of the emulation hot path's way (~5.7 billion steps).
		switch cpu.pc {
		case 0x0000: // warm boot: exerciser finished
			output := out.String()
			t.Logf("output:\n%s", output)
			if strings.Contains(output, "ERROR") {
				t.Fatalf("exerciser reported errors")
			}
			if !strings.Contains(output, "Tests complete") {
				t.Fatalf("exerciser did not run to completion")
			}
			return
		case 0x0005: // BDOS call
			switch cpu.c {
			case 2:
				out.WriteByte(cpu.e)
			case 9:
				for addr := cpu.de(); bus.mem[addr] != '$'; addr++ {
					out.WriteByte(bus.mem[addr])
				}
			}
			// RET: pop the return address.
			cpu.pc = uint16(bus.mem[cpu.sp]) | uint16(bus.mem[cpu.sp+1])<<8
			cpu.sp += 2
		}
		if cpu.tstates > limit {
			t.Fatalf("no completion after %d T-states; output so far:\n%s", limit, out.String())
		}
		cpu.Step()
	}
}

func TestZexdoc(t *testing.T) { t.Parallel(); runZex(t, "testdata/zex/zexdoc.com") }
func TestZexall(t *testing.T) { t.Parallel(); runZex(t, "testdata/zex/zexall.com") }
