// Package disasm decodes Z80 machine code into mnemonic text.
//
// [Decode] turns any byte sequence into exactly one instruction with a
// correct byte length — documented and undocumented opcodes, every
// prefix combination. It never fails and always makes progress
// (Len >= 1), so a debugger walking arbitrary memory cannot panic,
// stall, or mis-frame the instructions that follow.
//
// # Text conventions
//
// Mnemonics and register names are uppercase (LD, ADD, IX, AF').
// Operands are uppercase hex with no 0x prefix: 16-bit values as four
// digits (LD HL,2BA6), 8-bit as two (LD B,3F, RST 38). Relative jumps
// (JR, JR cc, DJNZ) show the resolved absolute target rather than the
// raw displacement: JR NZ,0038. Index displacements are signed hex:
// LD A,(IX+05), LD A,(IX-1B). No space follows a comma; a single space
// follows the mnemonic. The undocumented IX/IY register halves are
// written IXH, IXL, IYH, IYL (LD IXH,3F, ADD A,IYL); the undocumented
// DDCB/FDCB result-copy encodings use the explicit three-operand style
// RES 0,(IX+05),B, while the plain (IX+d)-only column renders without
// the copy register.
//
// # Placeholders
//
// Two classes of byte sequence have no assigned meaning and decode to
// documented placeholders:
//
//   - An ED-page opcode with neither a documented nor an accepted
//     undocumented meaning (the CPU executes it as an 8 T-state no-op)
//     decodes as the 2-byte instruction "NOP* ED xx", where xx is the
//     second opcode byte in hex.
//   - A DD or FD prefix followed by a byte to which the prefix assigns
//     no meaning — another DD/FD, the ED page, or an opcode that does
//     not involve HL, H, L or (HL), such as DD 00 (and the one true
//     exception, EX DE,HL, which the prefix never affects) — decodes
//     as the 1-byte instruction "NONI* DD" or "NONI* FD". The following
//     instruction then decodes on its own. This mirrors the silicon,
//     where a meaningless prefix acts as a NOP whose only effect is to
//     defer interrupt sampling, and it guarantees that a decoder
//     walking a stream always advances and never mis-frames what
//     follows a prefix run.
//
// # Reads
//
// Decode calls read only for the bytes of the instruction itself,
// addr through addr+Len-1, with one necessary exception: classifying a
// DD/FD prefix as redundant requires looking at the byte after it, so
// decoding a redundant prefix (Len 1) also reads the byte at addr+1.
// Reads must be side-effect free. Addresses wrap at 0xFFFF.
package disasm

// Instr is one decoded instruction.
type Instr struct {
	Addr  uint16  // address of the first byte
	Len   int     // bytes consumed (1..4); always > 0, so decoding a stream always makes progress
	Text  string  // e.g. "LD A,(2C36)", "JR NZ,0038" — uppercase, hex operands, no 0x prefix
	Bytes [4]byte // the raw instruction bytes; Bytes[:Len] are valid
}

const hexDigits = "0123456789ABCDEF"

// Decode decodes one instruction starting at addr, fetching bytes via
// read. Addresses wrap at 0xFFFF (read(addr), read(addr+1), ... with
// uint16 wraparound). It never fails: undefined encodings decode to a
// documented placeholder with the correct length (see the package
// comment).
func Decode(read func(uint16) byte, addr uint16) Instr {
	ins := Instr{Addr: addr}
	n := 0
	next := func() byte {
		b := read(addr + uint16(n))
		ins.Bytes[n] = b
		n++
		return b
	}

	// Select the text template. Lowercase marker bytes in a template
	// stand for operand bytes still to be fetched (see tables.go);
	// everything else is literal text.
	var tmpl string
	var idxDisp byte // displacement for DDCB/FDCB, fetched before the selector
	haveIdxDisp := false

	switch op := next(); op {
	case 0xCB:
		tmpl = cbText[next()]
	case 0xED:
		tmpl = edText[next()]
	case 0xDD, 0xFD:
		tabs := &ixTabs
		if op == 0xFD {
			tabs = &iyTabs
		}
		// One byte of lookahead decides whether the prefix means
		// anything; it is consumed only if it does.
		switch op2 := read(addr + uint16(n)); {
		case op2 == 0xCB: // DD CB d xx / FD CB d xx
			next()           // the CB byte
			idxDisp = next() // displacement precedes the selector byte
			haveIdxDisp = true
			tmpl = tabs.cb[next()]
		case tabs.main[op2] != "":
			next()
			tmpl = tabs.main[op2]
		default: // redundant prefix: a 1-byte placeholder instruction
			tmpl = tabs.noni
		}
	default:
		tmpl = baseText[op]
	}

	// Expand the template, fetching operand bytes as markers demand.
	var buf [24]byte
	w := 0
	expanded := false
	for i := 0; i < len(tmpl); i++ {
		switch m := tmpl[i]; m {
		case 'n': // 8-bit immediate
			b := next()
			buf[w] = hexDigits[b>>4]
			buf[w+1] = hexDigits[b&0xF]
			w += 2
			expanded = true
		case 'w': // 16-bit immediate, little-endian in memory
			lo := next()
			hi := next()
			buf[w] = hexDigits[hi>>4]
			buf[w+1] = hexDigits[hi&0xF]
			buf[w+2] = hexDigits[lo>>4]
			buf[w+3] = hexDigits[lo&0xF]
			w += 4
			expanded = true
		case 'e': // relative displacement, shown as the absolute target
			d := next()
			t := addr + uint16(n) + uint16(int8(d))
			buf[w] = hexDigits[t>>12]
			buf[w+1] = hexDigits[t>>8&0xF]
			buf[w+2] = hexDigits[t>>4&0xF]
			buf[w+3] = hexDigits[t&0xF]
			w += 4
			expanded = true
		case 'd': // signed index displacement: +xx / -xx
			b := idxDisp
			if !haveIdxDisp {
				b = next()
			}
			if b&0x80 != 0 {
				buf[w] = '-'
				b = -b
			} else {
				buf[w] = '+'
			}
			buf[w+1] = hexDigits[b>>4]
			buf[w+2] = hexDigits[b&0xF]
			w += 3
			expanded = true
		default:
			buf[w] = m
			w++
		}
	}
	ins.Len = n
	if expanded {
		ins.Text = string(buf[:w])
	} else {
		ins.Text = tmpl // no operands: reuse the interned table string
	}
	return ins
}
