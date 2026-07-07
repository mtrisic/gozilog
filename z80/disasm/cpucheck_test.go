package disasm_test

import (
	"strings"
	"testing"

	"github.com/mtrisic/gozilog/z80"
	"github.com/mtrisic/gozilog/z80/disasm"
)

// flatBus is 64 KB of flat RAM with idle I/O, the reference bus for
// the length cross-check.
type flatBus struct{ mem [65536]byte }

func (b *flatBus) MemRead(addr uint16) byte        { return b.mem[addr] }
func (b *flatBus) MemWrite(addr uint16, data byte) { b.mem[addr] = data }
func (b *flatBus) IORead(port uint16) byte         { return 0xFF }
func (b *flatBus) IOWrite(port uint16, data byte)  {}

// mnemonic returns the first word of an instruction text.
func mnemonic(text string) string {
	if i := strings.IndexByte(text, ' '); i >= 0 {
		return text[:i]
	}
	return text
}

// controlFlow lists the mnemonics whose PC advance is not the
// instruction length; they are excluded from the cross-check.
var controlFlow = map[string]bool{
	"JP": true, "JR": true, "CALL": true, "RET": true, "RETI": true,
	"RETN": true, "RST": true, "DJNZ": true, "HALT": true,
}

// TestLengthAgainstCPU cross-checks every decoded instruction length
// against the SingleStepTests-verified CPU core: execute the pattern
// on a flat 64 KB bus and require the PC advance to equal the decoded
// length. Redundant DD/FD prefixes decode as 1-byte placeholders while
// the CPU consumes a whole prefix run in one Step, so the decoder is
// re-applied until it produces a non-placeholder instruction and the
// summed lengths must match the CPU's consumption.
func TestLengthAgainstCPU(t *testing.T) {
	const org = 0x4000
	bus := &flatBus{}
	cpu := z80.New(bus)
	read := func(a uint16) byte { return bus.mem[a] }

	checked := 0
	for _, p := range patterns() {
		// Fresh memory: the pattern at org, NOP filler everywhere else.
		bus.mem = [65536]byte{}
		copy(bus.mem[org:], p)

		// Decode through any redundant-prefix placeholders.
		total := 0
		var last disasm.Instr
		for a := uint16(org); ; {
			last = disasm.Decode(read, a)
			total += last.Len
			a += uint16(last.Len)
			if !strings.HasPrefix(last.Text, "NONI*") {
				break
			}
			if total > 8 {
				t.Fatalf("% X: prefix chain did not terminate", p)
			}
		}
		if controlFlow[mnemonic(last.Text)] {
			continue
		}

		// The repeating block forms re-execute (PC -= 2) until their
		// counter hits zero, so give each its terminating counter:
		// BC=1 for the BC-counted LDxR/CPxR, B=1 for the B-counted
		// I/O forms.
		bc := uint16(0x0001)
		switch last.Text {
		case "INIR", "INDR", "OTIR", "OTDR":
			bc = 0x0100
		}
		cpu.SetState(z80.State{
			BC: bc, DE: 0x2000, HL: 0x8000,
			IX: 0x9000, IY: 0xA000,
			SP: 0xF000, PC: org,
		})
		cpu.Step()
		if got := int(cpu.State().PC - org); got != total {
			t.Errorf("% X: decoder says %d byte(s), CPU consumed %d (last %q)",
				p, total, got, last.Text)
		}
		checked++
	}
	t.Logf("cross-checked %d of %d patterns against the CPU", checked, len(patterns()))
}
