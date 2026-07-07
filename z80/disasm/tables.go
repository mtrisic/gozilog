package disasm

// Text templates per prefix page. Lowercase bytes are operand markers
// expanded by Decode ('n' 8-bit immediate, 'w' 16-bit immediate,
// 'e' relative-jump displacement rendered as its absolute target,
// 'd' signed index displacement); instruction text proper is all
// uppercase, so the two can never collide.

// baseText holds the 256 unprefixed opcodes. The prefix bytes
// 0xCB/0xDD/0xED/0xFD are dispatched before this table is consulted;
// their entries are empty.
var baseText = [256]string{
	// 0x00
	"NOP", "LD BC,w", "LD (BC),A", "INC BC", "INC B", "DEC B", "LD B,n", "RLCA",
	"EX AF,AF'", "ADD HL,BC", "LD A,(BC)", "DEC BC", "INC C", "DEC C", "LD C,n", "RRCA",
	// 0x10
	"DJNZ e", "LD DE,w", "LD (DE),A", "INC DE", "INC D", "DEC D", "LD D,n", "RLA",
	"JR e", "ADD HL,DE", "LD A,(DE)", "DEC DE", "INC E", "DEC E", "LD E,n", "RRA",
	// 0x20
	"JR NZ,e", "LD HL,w", "LD (w),HL", "INC HL", "INC H", "DEC H", "LD H,n", "DAA",
	"JR Z,e", "ADD HL,HL", "LD HL,(w)", "DEC HL", "INC L", "DEC L", "LD L,n", "CPL",
	// 0x30
	"JR NC,e", "LD SP,w", "LD (w),A", "INC SP", "INC (HL)", "DEC (HL)", "LD (HL),n", "SCF",
	"JR C,e", "ADD HL,SP", "LD A,(w)", "DEC SP", "INC A", "DEC A", "LD A,n", "CCF",
	// 0x40
	"LD B,B", "LD B,C", "LD B,D", "LD B,E", "LD B,H", "LD B,L", "LD B,(HL)", "LD B,A",
	"LD C,B", "LD C,C", "LD C,D", "LD C,E", "LD C,H", "LD C,L", "LD C,(HL)", "LD C,A",
	// 0x50
	"LD D,B", "LD D,C", "LD D,D", "LD D,E", "LD D,H", "LD D,L", "LD D,(HL)", "LD D,A",
	"LD E,B", "LD E,C", "LD E,D", "LD E,E", "LD E,H", "LD E,L", "LD E,(HL)", "LD E,A",
	// 0x60
	"LD H,B", "LD H,C", "LD H,D", "LD H,E", "LD H,H", "LD H,L", "LD H,(HL)", "LD H,A",
	"LD L,B", "LD L,C", "LD L,D", "LD L,E", "LD L,H", "LD L,L", "LD L,(HL)", "LD L,A",
	// 0x70
	"LD (HL),B", "LD (HL),C", "LD (HL),D", "LD (HL),E", "LD (HL),H", "LD (HL),L", "HALT", "LD (HL),A",
	"LD A,B", "LD A,C", "LD A,D", "LD A,E", "LD A,H", "LD A,L", "LD A,(HL)", "LD A,A",
	// 0x80
	"ADD A,B", "ADD A,C", "ADD A,D", "ADD A,E", "ADD A,H", "ADD A,L", "ADD A,(HL)", "ADD A,A",
	"ADC A,B", "ADC A,C", "ADC A,D", "ADC A,E", "ADC A,H", "ADC A,L", "ADC A,(HL)", "ADC A,A",
	// 0x90
	"SUB B", "SUB C", "SUB D", "SUB E", "SUB H", "SUB L", "SUB (HL)", "SUB A",
	"SBC A,B", "SBC A,C", "SBC A,D", "SBC A,E", "SBC A,H", "SBC A,L", "SBC A,(HL)", "SBC A,A",
	// 0xA0
	"AND B", "AND C", "AND D", "AND E", "AND H", "AND L", "AND (HL)", "AND A",
	"XOR B", "XOR C", "XOR D", "XOR E", "XOR H", "XOR L", "XOR (HL)", "XOR A",
	// 0xB0
	"OR B", "OR C", "OR D", "OR E", "OR H", "OR L", "OR (HL)", "OR A",
	"CP B", "CP C", "CP D", "CP E", "CP H", "CP L", "CP (HL)", "CP A",
	// 0xC0
	"RET NZ", "POP BC", "JP NZ,w", "JP w", "CALL NZ,w", "PUSH BC", "ADD A,n", "RST 00",
	"RET Z", "RET", "JP Z,w", "", "CALL Z,w", "CALL w", "ADC A,n", "RST 08",
	// 0xD0
	"RET NC", "POP DE", "JP NC,w", "OUT (n),A", "CALL NC,w", "PUSH DE", "SUB n", "RST 10",
	"RET C", "EXX", "JP C,w", "IN A,(n)", "CALL C,w", "", "SBC A,n", "RST 18",
	// 0xE0
	"RET PO", "POP HL", "JP PO,w", "EX (SP),HL", "CALL PO,w", "PUSH HL", "AND n", "RST 20",
	"RET PE", "JP (HL)", "JP PE,w", "EX DE,HL", "CALL PE,w", "", "XOR n", "RST 28",
	// 0xF0
	"RET P", "POP AF", "JP P,w", "DI", "CALL P,w", "PUSH AF", "OR n", "RST 30",
	"RET M", "LD SP,HL", "JP M,w", "EI", "CALL M,w", "", "CP n", "RST 38",
}

var (
	cbText [256]string // CB page, indexed by the second byte
	edText [256]string // ED page, indexed by the second byte
	ixTabs indexTables // DD prefix
	iyTabs indexTables // FD prefix
)

// indexTables holds the DD- or FD-prefixed decode space.
type indexTables struct {
	main [256]string // by the byte after the prefix; "" = prefix assigns no meaning
	cb   [256]string // DDCB/FDCB page, indexed by the final (selector) byte
	noni string      // 1-byte placeholder for a redundant prefix
}

var regs8 = [8]string{"B", "C", "D", "E", "H", "L", "(HL)", "A"}
var rotOps = [8]string{"RLC", "RRC", "RL", "RR", "SLA", "SRA", "SLL", "SRL"}

// aluOps carries the conventional spelling of each ALU operation's
// leading text (SUB, AND, XOR, OR and CP take no explicit A operand).
var aluOps = [8]string{"ADD A,", "ADC A,", "SUB ", "SBC A,", "AND ", "XOR ", "OR ", "CP "}

func hexByte(b byte) string {
	return string([]byte{hexDigits[b>>4], hexDigits[b&0xF]})
}

func init() {
	buildCB()
	buildED()
	buildIndex(&ixTabs, "IX")
	buildIndex(&iyTabs, "IY")
	ixTabs.noni = "NONI* DD"
	iyTabs.noni = "NONI* FD"
}

// buildCB fills the CB page: rotates/shifts (including the
// undocumented SLL), BIT, RES, SET over the standard register column.
func buildCB() {
	for i := 0; i < 256; i++ {
		x, y, z := i>>6, i>>3&7, i&7
		bit := string(byte('0' + y))
		switch x {
		case 0:
			cbText[i] = rotOps[y] + " " + regs8[z]
		case 1:
			cbText[i] = "BIT " + bit + "," + regs8[z]
		case 2:
			cbText[i] = "RES " + bit + "," + regs8[z]
		case 3:
			cbText[i] = "SET " + bit + "," + regs8[z]
		}
	}
}

// buildED fills the ED page: the 0x40–0x7F quadrant, the block
// operations, and the "NOP* ED xx" placeholder everywhere else.
// Accepted undocumented forms follow convention: NEG and RETN
// duplicates, IM duplicates, IN (C) for ED 70 and OUT (C),0 for ED 71.
func buildED() {
	for i := 0; i < 256; i++ {
		edText[i] = "NOP* ED " + hexByte(byte(i))
	}
	rp := [4]string{"BC", "DE", "HL", "SP"}
	im := [8]string{"0", "0", "1", "2", "0", "0", "1", "2"}
	for i := 0x40; i <= 0x7F; i++ {
		y, z := i>>3&7, i&7
		p, q := y>>1, y&1
		switch z {
		case 0:
			if y == 6 {
				edText[i] = "IN (C)"
			} else {
				edText[i] = "IN " + regs8[y] + ",(C)"
			}
		case 1:
			if y == 6 {
				edText[i] = "OUT (C),0"
			} else {
				edText[i] = "OUT (C)," + regs8[y]
			}
		case 2:
			if q == 0 {
				edText[i] = "SBC HL," + rp[p]
			} else {
				edText[i] = "ADC HL," + rp[p]
			}
		case 3:
			if q == 0 {
				edText[i] = "LD (w)," + rp[p]
			} else {
				edText[i] = "LD " + rp[p] + ",(w)"
			}
		case 4:
			edText[i] = "NEG"
		case 5:
			if i == 0x4D {
				edText[i] = "RETI"
			} else {
				edText[i] = "RETN"
			}
		case 6:
			edText[i] = "IM " + im[y]
		case 7:
			switch y {
			case 0:
				edText[i] = "LD I,A"
			case 1:
				edText[i] = "LD R,A"
			case 2:
				edText[i] = "LD A,I"
			case 3:
				edText[i] = "LD A,R"
			case 4:
				edText[i] = "RRD"
			case 5:
				edText[i] = "RLD"
				// y == 6, 7 (ED 77, ED 7F): keep the NOP* placeholder.
			}
		}
	}
	edText[0xA0], edText[0xA1], edText[0xA2], edText[0xA3] = "LDI", "CPI", "INI", "OUTI"
	edText[0xA8], edText[0xA9], edText[0xAA], edText[0xAB] = "LDD", "CPD", "IND", "OUTD"
	edText[0xB0], edText[0xB1], edText[0xB2], edText[0xB3] = "LDIR", "CPIR", "INIR", "OTIR"
	edText[0xB8], edText[0xB9], edText[0xBA], edText[0xBB] = "LDDR", "CPDR", "INDR", "OTDR"
}

// buildIndex fills one DD/FD decode space for the named index
// register. main holds the opcodes the prefix affects (HL→IX/IY,
// H/L→IXH/IXL etc., (HL)→(IX+d)); the empty entries are the opcodes to
// which the prefix assigns no meaning, decoded as the NONI placeholder.
// EX DE,HL (0xEB) and HALT (0x76) are deliberately unaffected, and
// JP (HL)/EX (SP),HL/LD SP,HL remap without gaining a displacement.
func buildIndex(t *indexTables, xy string) {
	// The undocumented register column with H/L replaced by the index
	// halves; slot 6 unused here.
	iregs := [8]string{"B", "C", "D", "E", xy + "H", xy + "L", "", "A"}
	mem := "(" + xy + "d)" // (IX+d) / (IY+d) with the displacement marker

	for i := 0; i < 256; i++ {
		x, y, z := i>>6, i>>3&7, i&7
		var s string
		switch {
		case i == 0x09 || i == 0x19 || i == 0x39:
			s = "ADD " + xy + "," + [4]string{"BC", "DE", "", "SP"}[y>>1]
		case i == 0x29:
			s = "ADD " + xy + "," + xy
		case i == 0x21:
			s = "LD " + xy + ",w"
		case i == 0x22:
			s = "LD (w)," + xy
		case i == 0x2A:
			s = "LD " + xy + ",(w)"
		case i == 0x23:
			s = "INC " + xy
		case i == 0x2B:
			s = "DEC " + xy
		case i == 0x24:
			s = "INC " + xy + "H"
		case i == 0x25:
			s = "DEC " + xy + "H"
		case i == 0x26:
			s = "LD " + xy + "H,n"
		case i == 0x2C:
			s = "INC " + xy + "L"
		case i == 0x2D:
			s = "DEC " + xy + "L"
		case i == 0x2E:
			s = "LD " + xy + "L,n"
		case i == 0x34:
			s = "INC " + mem
		case i == 0x35:
			s = "DEC " + mem
		case i == 0x36:
			s = "LD " + mem + ",n"
		case x == 1 && i != 0x76: // LD r,r' quadrant
			switch {
			case y == 6:
				s = "LD " + mem + "," + regs8[z]
			case z == 6:
				s = "LD " + regs8[y] + "," + mem
			case y == 4 || y == 5 || z == 4 || z == 5:
				s = "LD " + iregs[y] + "," + iregs[z]
			}
		case x == 2: // ALU quadrant
			switch {
			case z == 6:
				s = aluOps[y] + mem
			case z == 4 || z == 5:
				s = aluOps[y] + iregs[z]
			}
		case i == 0xE1:
			s = "POP " + xy
		case i == 0xE3:
			s = "EX (SP)," + xy
		case i == 0xE5:
			s = "PUSH " + xy
		case i == 0xE9:
			s = "JP (" + xy + ")"
		case i == 0xF9:
			s = "LD SP," + xy
		}
		t.main[i] = s
	}

	// The DDCB/FDCB page, indexed by the selector byte that follows
	// the displacement. Undocumented encodings with a register column
	// other than (HL) copy the result into that register and render as
	// e.g. "RLC (IX+05),B"; BIT has no result to copy, so all eight
	// encodings render alike.
	for i := 0; i < 256; i++ {
		x, y, z := i>>6, i>>3&7, i&7
		bit := string(byte('0' + y))
		copyReg := ""
		if z != 6 {
			copyReg = "," + regs8[z]
		}
		switch x {
		case 0:
			t.cb[i] = rotOps[y] + " " + mem + copyReg
		case 1:
			t.cb[i] = "BIT " + bit + "," + mem
		case 2:
			t.cb[i] = "RES " + bit + "," + mem + copyReg
		case 3:
			t.cb[i] = "SET " + bit + "," + mem + copyReg
		}
	}
}
