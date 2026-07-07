package disasm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mtrisic/gozilog/z80/disasm"
)

// readerFor serves the pattern bytes at addr, then endless filler
// bytes, and records the highest offset actually read.
func readerFor(pattern []byte, addr uint16, filler byte, maxOff *int) func(uint16) byte {
	return func(a uint16) byte {
		off := int(a - addr) // uint16 wraparound, then small positive
		if off > *maxOff {
			*maxOff = off
		}
		if off < len(pattern) {
			return pattern[off]
		}
		return filler
	}
}

// patterns enumerates every starting opcode pattern: all single bytes,
// the full CB/ED/DD/FD pages, and the full DDCB/FDCB pages with three
// representative displacements.
func patterns() [][]byte {
	var ps [][]byte
	for i := 0; i < 256; i++ {
		ps = append(ps, []byte{byte(i)})
	}
	for _, pre := range []byte{0xCB, 0xED, 0xDD, 0xFD} {
		for i := 0; i < 256; i++ {
			ps = append(ps, []byte{pre, byte(i)})
		}
	}
	for _, pre := range []byte{0xDD, 0xFD} {
		for _, d := range []byte{0x00, 0x7F, 0x80} {
			for i := 0; i < 256; i++ {
				ps = append(ps, []byte{pre, 0xCB, d, byte(i)})
			}
		}
	}
	return ps
}

// TestExhaustiveSweep decodes every opcode pattern and asserts the
// stream-walking invariants: no panic, 1 <= Len <= 4, Bytes echoes the
// input, Text non-empty, and reads confined to the instruction (plus
// the single documented lookahead byte for a redundant DD/FD prefix).
func TestExhaustiveSweep(t *testing.T) {
	const addr, filler = 0x0100, 0x34
	for _, p := range patterns() {
		maxOff := 0
		ins := disasm.Decode(readerFor(p, addr, filler, &maxOff), addr)
		if ins.Len < 1 || ins.Len > 4 {
			t.Fatalf("% X: Len = %d, want 1..4", p, ins.Len)
		}
		if ins.Text == "" {
			t.Fatalf("% X: empty Text", p)
		}
		if ins.Addr != addr {
			t.Fatalf("% X: Addr = %04X, want %04X", p, ins.Addr, addr)
		}
		for i := 0; i < ins.Len; i++ {
			want := byte(filler)
			if i < len(p) {
				want = p[i]
			}
			if ins.Bytes[i] != want {
				t.Fatalf("% X: Bytes[%d] = %02X, want %02X (text %q)",
					p, i, ins.Bytes[i], want, ins.Text)
			}
		}
		limit := ins.Len - 1
		if strings.HasPrefix(ins.Text, "NONI*") {
			limit = ins.Len // the one allowed lookahead byte
		}
		if maxOff > limit {
			t.Fatalf("% X: read up to offset %d, limit %d (text %q)",
				p, maxOff, limit, ins.Text)
		}
	}
}

// TestSpot pins down the text conventions with hand-written
// expectations across every instruction family.
func TestSpot(t *testing.T) {
	tests := []struct {
		addr  uint16
		bytes []byte
		text  string
		len   int
	}{
		// Unprefixed basics.
		{0x0000, []byte{0x00}, "NOP", 1},
		{0x0000, []byte{0x01, 0x34, 0x12}, "LD BC,1234", 3},
		{0x0000, []byte{0x06, 0x3F}, "LD B,3F", 2},
		{0x0000, []byte{0x21, 0xA6, 0x2B}, "LD HL,2BA6", 3},
		{0x0000, []byte{0x22, 0xA6, 0x2B}, "LD (2BA6),HL", 3},
		{0x0000, []byte{0x3A, 0x36, 0x2C}, "LD A,(2C36)", 3},
		{0x0000, []byte{0x08}, "EX AF,AF'", 1},
		{0x0000, []byte{0xEB}, "EX DE,HL", 1},
		{0x0000, []byte{0xD9}, "EXX", 1},
		{0x0000, []byte{0x76}, "HALT", 1},
		{0x0000, []byte{0xC7}, "RST 00", 1},
		{0x0000, []byte{0xFF}, "RST 38", 1},
		{0x0000, []byte{0xDB, 0xFE}, "IN A,(FE)", 2},
		{0x0000, []byte{0xD3, 0xFE}, "OUT (FE),A", 2},
		{0x0000, []byte{0x96}, "SUB (HL)", 1},
		{0x0000, []byte{0xC6, 0x12}, "ADD A,12", 2},
		// Relative jumps resolve to the absolute target.
		{0x0038, []byte{0x20, 0xFE}, "JR NZ,0038", 2},
		{0x0000, []byte{0x18, 0x05}, "JR 0007", 2},
		{0x0100, []byte{0x10, 0xFD}, "DJNZ 00FF", 2},
		{0xFFFE, []byte{0x18, 0x06}, "JR 0006", 2}, // wraps past 0xFFFF
		// CB page, including undocumented SLL.
		{0x0000, []byte{0xCB, 0x7E}, "BIT 7,(HL)", 2},
		{0x0000, []byte{0xCB, 0x30}, "SLL B", 2},
		{0x0000, []byte{0xCB, 0x06}, "RLC (HL)", 2},
		{0x0000, []byte{0xCB, 0xC7}, "SET 0,A", 2},
		// ED page, documented and accepted-undocumented.
		{0x0000, []byte{0xED, 0x56}, "IM 1", 2},
		{0x0000, []byte{0xED, 0x43, 0x34, 0x12}, "LD (1234),BC", 4},
		{0x0000, []byte{0xED, 0x5A}, "ADC HL,DE", 2},
		{0x0000, []byte{0xED, 0xB0}, "LDIR", 2},
		{0x0000, []byte{0xED, 0x5F}, "LD A,R", 2},
		{0x0000, []byte{0xED, 0x44}, "NEG", 2},
		{0x0000, []byte{0xED, 0x4C}, "NEG", 2}, // undocumented duplicate
		{0x0000, []byte{0xED, 0x45}, "RETN", 2},
		{0x0000, []byte{0xED, 0x4D}, "RETI", 2},
		{0x0000, []byte{0xED, 0x5D}, "RETN", 2}, // undocumented duplicate
		{0x0000, []byte{0xED, 0x70}, "IN (C)", 2},
		{0x0000, []byte{0xED, 0x71}, "OUT (C),0", 2},
		{0x0000, []byte{0xED, 0x77}, "NOP* ED 77", 2}, // undefined placeholder
		{0x0000, []byte{0xED, 0x0E}, "NOP* ED 0E", 2},
		// DD/FD pages, documented and undocumented halves.
		{0x0000, []byte{0xDD, 0x26, 0x3F}, "LD IXH,3F", 3},
		{0x0000, []byte{0xFD, 0x85}, "ADD A,IYL", 2},
		{0x0000, []byte{0xDD, 0x7E, 0x05}, "LD A,(IX+05)", 3},
		{0x0000, []byte{0xDD, 0x7E, 0xE5}, "LD A,(IX-1B)", 3},
		{0x0000, []byte{0xDD, 0x36, 0x0A, 0x12}, "LD (IX+0A),12", 4},
		{0x0000, []byte{0xDD, 0x29}, "ADD IX,IX", 2},
		{0x0000, []byte{0xDD, 0xE9}, "JP (IX)", 2},
		{0x0000, []byte{0xFD, 0xE3}, "EX (SP),IY", 2},
		{0x0000, []byte{0xFD, 0x66, 0x80}, "LD H,(IY-80)", 3},
		// Redundant prefixes decode as a 1-byte placeholder.
		{0x0000, []byte{0xDD, 0xDD, 0x00}, "NONI* DD", 1},
		{0x0000, []byte{0xDD, 0x00}, "NONI* DD", 1},
		{0x0000, []byte{0xDD, 0xED, 0x44}, "NONI* DD", 1},
		{0x0000, []byte{0xFD, 0xEB}, "NONI* FD", 1}, // EX DE,HL is never indexed
		// DDCB/FDCB: displacement before the selector, result-copy style.
		{0x0000, []byte{0xDD, 0xCB, 0x05, 0x80}, "RES 0,(IX+05),B", 4},
		{0x0000, []byte{0xFD, 0xCB, 0xFF, 0x07}, "RLC (IY-01),A", 4},
		{0x0000, []byte{0xDD, 0xCB, 0x7F, 0x7E}, "BIT 7,(IX+7F)", 4},
		{0x0000, []byte{0xDD, 0xCB, 0x80, 0x3E}, "SRL (IX-80)", 4},
		{0x0000, []byte{0xFD, 0xCB, 0x02, 0xC6}, "SET 0,(IY+02)", 4},
	}
	for _, tt := range tests {
		maxOff := 0
		ins := disasm.Decode(readerFor(tt.bytes, tt.addr, 0x00, &maxOff), tt.addr)
		if ins.Text != tt.text || ins.Len != tt.len {
			t.Errorf("% X @ %04X: got %q len %d, want %q len %d",
				tt.bytes, tt.addr, ins.Text, ins.Len, tt.text, tt.len)
		}
	}
}

func ExampleDecode() {
	code := []byte{
		0x21, 0xA6, 0x2B, // LD HL,2BA6
		0xDD, 0x7E, 0x05, // LD A,(IX+05)
		0x18, 0xFE, //       JR (self)
	}
	const org = 0x8000
	read := func(a uint16) byte { return code[a-org] }
	for addr := uint16(org); addr < org+uint16(len(code)); {
		ins := disasm.Decode(read, addr)
		fmt.Printf("%04X  %s\n", ins.Addr, ins.Text)
		addr += uint16(ins.Len)
	}
	// Output:
	// 8000  LD HL,2BA6
	// 8003  LD A,(IX+05)
	// 8006  JR 8006
}
