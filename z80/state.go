package z80

// State is a complete snapshot of CPU state — every register and every
// piece of hidden state, sufficient for save states and for driving
// state-level verification suites. [CPU.SetState] followed by identical
// bus behavior reproduces execution byte-for-byte.
//
// 16-bit fields hold register pairs in the conventional high/low order
// (e.g. AF == uint16(A)<<8 | uint16(F)).
type State struct {
	AF, BC, DE, HL     uint16 // main register pairs
	AF2, BC2, DE2, HL2 uint16 // shadow register pairs (AF', BC', DE', HL')
	IX, IY             uint16 // index registers
	SP, PC             uint16 // stack pointer, program counter

	// WZ is the internal address latch also known as MEMPTR. It affects
	// the undocumented X/Y flag results of BIT n,(HL) and is observable
	// through them.
	WZ uint16

	I byte // interrupt vector base register
	R byte // memory refresh register (7-bit counter + preserved bit 7)

	IM   byte // interrupt mode: 0, 1, or 2
	IFF1 bool // interrupt enable flip-flop (masks INT)
	IFF2 bool // interrupt enable backup (readable via LD A,I / LD A,R)

	// Halted reports whether the CPU has executed HALT and is waiting
	// for an interrupt or reset.
	Halted bool

	// EIPending reports that the previous instruction was EI, which
	// defers interrupt acceptance for one instruction.
	EIPending bool

	// Q latches whether the previous instruction modified the flag
	// register; it determines the undocumented X/Y flag behavior of
	// SCF and CCF. Zero means flags were not modified.
	Q byte

	// P latches whether the previous instruction was LD A,I or LD A,R,
	// which exhibit the "IFF2 read during INT" erratum. Matches the
	// "p" field of the SingleStepTests state records.
	P byte
}

// State returns a snapshot of the complete CPU state.
func (c *CPU) State() State {
	return State{
		AF:  uint16(c.a)<<8 | uint16(c.f),
		BC:  uint16(c.b)<<8 | uint16(c.c),
		DE:  uint16(c.d)<<8 | uint16(c.e),
		HL:  uint16(c.h)<<8 | uint16(c.l),
		AF2: uint16(c.a2)<<8 | uint16(c.f2),
		BC2: uint16(c.b2)<<8 | uint16(c.c2),
		DE2: uint16(c.d2)<<8 | uint16(c.e2),
		HL2: uint16(c.h2)<<8 | uint16(c.l2),
		IX:  uint16(c.ixh)<<8 | uint16(c.ixl),
		IY:  uint16(c.iyh)<<8 | uint16(c.iyl),
		SP:  c.sp, PC: c.pc, WZ: c.wz,
		I: c.i, R: c.r,
		IM: c.im, IFF1: c.iff1, IFF2: c.iff2,
		Halted: c.halted, EIPending: c.eiPending,
		Q: c.q, P: c.p,
	}
}

// SetState restores the complete CPU state from a snapshot. It does not
// touch the bus or the T-state counter.
func (c *CPU) SetState(s State) {
	c.a, c.f = byte(s.AF>>8), byte(s.AF)
	c.b, c.c = byte(s.BC>>8), byte(s.BC)
	c.d, c.e = byte(s.DE>>8), byte(s.DE)
	c.h, c.l = byte(s.HL>>8), byte(s.HL)
	c.a2, c.f2 = byte(s.AF2>>8), byte(s.AF2)
	c.b2, c.c2 = byte(s.BC2>>8), byte(s.BC2)
	c.d2, c.e2 = byte(s.DE2>>8), byte(s.DE2)
	c.h2, c.l2 = byte(s.HL2>>8), byte(s.HL2)
	c.ixh, c.ixl = byte(s.IX>>8), byte(s.IX)
	c.iyh, c.iyl = byte(s.IY>>8), byte(s.IY)
	c.sp, c.pc, c.wz = s.SP, s.PC, s.WZ
	c.i, c.r = s.I, s.R
	c.im, c.iff1, c.iff2 = s.IM, s.IFF1, s.IFF2
	c.halted, c.eiPending = s.Halted, s.EIPending
	c.q, c.p = s.Q, s.P
}
