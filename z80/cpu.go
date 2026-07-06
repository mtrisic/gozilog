package z80

// CPU is a Zilog Z80 CPU core. Create one with [New], drive it with
// [CPU.Step] or [CPU.Run], and signal interrupts with [CPU.SetINT] and
// [CPU.NMI]. All methods must be called from a single goroutine.
type CPU struct {
	bus    Bus
	ticker Ticker   // nil when the Bus does not implement Ticker
	acker  IntAcker // nil when the Bus does not implement IntAcker

	// Register file. Pairs are kept as separate bytes because the
	// instruction set addresses halves directly (including IXH/IXL,
	// IYH/IYL via undocumented opcodes).
	a, f, b, c, d, e, h, l         byte
	a2, f2, b2, c2, d2, e2, h2, l2 byte
	ixh, ixl, iyh, iyl             byte
	sp, pc                         uint16
	wz                             uint16 // MEMPTR
	i, r                           byte
	im                             byte
	iff1, iff2                     bool
	halted                         bool
	eiPending                      bool
	q, p                           byte

	// flagsSet records whether the currently executing instruction
	// modified F; after each instruction Q becomes F if set, else 0.
	flagsSet bool

	// idx is the index mode set by a DD/FD prefix for the current
	// instruction; dispValid/dispEA cache the (IX+d) effective address
	// so the displacement is fetched once per instruction.
	idx       idxMode
	dispValid bool
	dispEA    uint16

	// lastIR latches the refresh address (I<<8|R) emitted by the most
	// recent M1 cycle; instructions place it on the address bus during
	// their internal T-states (verified against SST cycle traces).
	lastIR uint16

	// im0 is set while executing an IM 0 injected instruction: all its
	// bytes (opcode, prefixes, operands) come from IntAck rather than
	// memory, and PC does not advance.
	im0 bool

	intLine    bool // level of the /INT line, set by SetINT
	nmiPending bool // latched NMI edge, cleared on acceptance

	tstates uint64
}

// New returns a CPU connected to bus, in the documented power-on state:
// AF, BC, DE, HL (and shadows), IX, IY and SP all 0xFFFF; PC, I, R zero;
// interrupts disabled; interrupt mode 0.
//
// New checks once whether bus also implements [Ticker] and [IntAcker];
// implementing them on a wrapper type after New will have no effect.
func New(bus Bus) *CPU {
	c := &CPU{bus: bus}
	c.ticker, _ = bus.(Ticker)
	c.acker, _ = bus.(IntAcker)
	c.powerOn()
	return c
}

func (c *CPU) powerOn() {
	c.SetState(State{
		AF: 0xFFFF, BC: 0xFFFF, DE: 0xFFFF, HL: 0xFFFF,
		AF2: 0xFFFF, BC2: 0xFFFF, DE2: 0xFFFF, HL2: 0xFFFF,
		IX: 0xFFFF, IY: 0xFFFF, SP: 0xFFFF,
	})
}

// Reset applies the /RESET sequence: PC, I, R and WZ cleared, interrupts
// disabled, interrupt mode 0, HALT state left. Other registers keep
// their values, as on real hardware. It consumes no T-states.
func (c *CPU) Reset() {
	c.pc, c.wz = 0, 0
	c.i, c.r = 0, 0
	c.im = 0
	c.iff1, c.iff2 = false, false
	c.halted = false
	c.eiPending = false
	c.q, c.p = 0, 0
}

// SpecialReset performs the "special RESET" documented by Tony Brewer
// (http://www.primrosebank.net/computers/z80/z80_special_reset.htm),
// triggered on real hardware by asserting /RESET for only 1–2 clock
// cycles: at the next instruction boundary PC is forced to 0x0000
// (leaving HALT if halted) while every other register — including I,
// R, IM, IFF1/IFF2 — is preserved, unlike a full [CPU.Reset]. The
// pending interrupt-accept state is untouched, so a pending NMI/INT is
// serviced from PC 0.
func (c *CPU) SpecialReset() {
	c.pc = 0
	c.wz = 0
	c.halted = false
}

// SetINT sets the level of the maskable interrupt line /INT. The line is
// level-triggered: it stays active until the machine clears it. The CPU
// samples it at instruction boundaries (in [CPU.Step]) and accepts when
// IFF1 is set and the previous instruction was not EI.
func (c *CPU) SetINT(active bool) { c.intLine = active }

// NMI latches a non-maskable interrupt edge. The CPU accepts it at the
// next instruction boundary regardless of IFF1, jumping to 0x0066.
func (c *CPU) NMI() { c.nmiPending = true }

// Tstates returns the monotonic T-state counter: the total number of
// T-states executed since New, including inserted wait states.
func (c *CPU) Tstates() uint64 { return c.tstates }

// Halted reports whether the CPU is in the HALT state.
func (c *CPU) Halted() bool { return c.halted }

// Step executes one instruction — or accepts one pending interrupt —
// and returns the number of T-states consumed, including wait states
// inserted by the machine's [Ticker].
func (c *CPU) Step() int {
	start := c.tstates

	deferINT := c.eiPending
	c.eiPending = false

	switch {
	case c.nmiPending:
		c.nmiPending = false
		c.acceptNMI()
	case c.intLine && c.iff1 && !deferINT:
		c.acceptINT()
	case c.halted:
		c.haltCycle()
	default:
		c.flagsSet = false
		c.p = 0 // set again only by LD A,I / LD A,R
		c.idx, c.dispValid = modeHL, false
		op := c.fetchOpcode()
		c.exec(op)
		if c.flagsSet {
			c.q = c.f
		} else {
			c.q = 0
		}
	}
	return int(c.tstates - start)
}

// Run executes instructions until at least tstates T-states have
// elapsed, then returns the number actually executed (which may exceed
// the budget by up to one instruction).
func (c *CPU) Run(tstates int) int {
	start := c.tstates
	for c.tstates-start < uint64(tstates) {
		c.Step()
	}
	return int(c.tstates - start)
}

// ---------------------------------------------------------------------
// Micro-operations. These own the timing model: every T-state of every
// M-cycle is emitted from here (and from the interrupt acceptance
// sequences below), never from individual opcode handlers. SPEC.md
// documents the schedule; Phase 3 aligns it T-state-exact with the
// SingleStepTests cycle traces.
// ---------------------------------------------------------------------

// tick emits one T-state and any wait states the machine requests.
func (c *CPU) tick(addr uint16, data int16, pins Pins) {
	c.tstates++
	if c.ticker != nil {
		for waits := c.ticker.Tick(addr, data, pins); waits > 0; waits-- {
			c.tstates++
			c.ticker.Tick(addr, data, pins)
		}
	}
}

// fetchOpcode performs an M1 cycle (4T): opcode read + refresh, R
// incremented, PC advanced. T-state schedule (verified against the
// SingleStepTests cycle traces): T1 address out; T2 MREQ|RD asserted
// (wait states sampled here); T3/T4 refresh with I<<8|R (pre-increment
// R) on the address bus and the fetched opcode riding the data bus on
// T3.
func (c *CPU) fetchOpcode() byte {
	if c.im0 {
		return c.intAckM1()
	}
	return c.fetchM1(M1)
}

// haltCycle is the M1 cycle executed while halted: a fetch at PC with
// the data discarded and PC not advanced, /HALT active, R incremented.
func (c *CPU) haltCycle() {
	pc := c.pc
	c.fetchM1(M1 | HALTP)
	c.pc = pc
}

// fetchM1 emits one opcode-fetch M-cycle with the given M1-role pins.
func (c *CPU) fetchM1(pins Pins) byte {
	c.tick(c.pc, DataNone, pins)         // T1
	c.tick(c.pc, DataNone, pins|MREQ|RD) // T2 (+waits)
	op := c.bus.MemRead(c.pc)
	c.pc++
	ir := uint16(c.i)<<8 | uint16(c.r)
	c.lastIR = ir
	c.r = c.r&0x80 | (c.r+1)&0x7F
	c.tick(ir, int16(op), pins&HALTP|RFSH) // T3
	c.tick(ir, DataNone, pins&HALTP|RFSH)  // T4
	return op
}

// read8 performs a memory read M-cycle (3T): T1 address out, T2
// MREQ|RD (waits sampled), T3 data on the bus.
func (c *CPU) read8(addr uint16) byte {
	c.tick(addr, DataNone, 0)       // T1
	c.tick(addr, DataNone, MREQ|RD) // T2 (+waits)
	v := c.bus.MemRead(addr)
	c.tick(addr, int16(v), 0) // T3
	return v
}

// write8 performs a memory write M-cycle (3T): T1 address out, T2
// MREQ|WR with the data on the bus (waits sampled), T3 quiet.
func (c *CPU) write8(addr uint16, v byte) {
	c.tick(addr, DataNone, 0)       // T1
	c.tick(addr, int16(v), MREQ|WR) // T2 (+waits)
	c.bus.MemWrite(addr, v)
	c.tick(addr, DataNone, 0) // T3
}

// fetch8 reads the next instruction byte (immediate operand or
// displacement) and advances PC. During IM 0 injection the byte comes
// from the interrupting device (a 3T acknowledge-shaped read) and PC
// stays put.
func (c *CPU) fetch8() byte {
	if c.im0 {
		c.tick(c.pc, DataNone, 0)       // T1
		c.tick(c.pc, DataNone, IORQ|RD) // T2 (+waits)
		v := c.intAck()
		c.tick(c.pc, int16(v), 0) // T3
		return v
	}
	v := c.read8(c.pc)
	c.pc++
	return v
}

// fetch16 reads a little-endian 16-bit immediate operand.
func (c *CPU) fetch16() uint16 {
	lo := c.fetch8()
	hi := c.fetch8()
	return uint16(hi)<<8 | uint16(lo)
}

// read16 reads a little-endian word from memory (two 3T cycles).
func (c *CPU) read16(addr uint16) uint16 {
	lo := c.read8(addr)
	hi := c.read8(addr + 1)
	return uint16(hi)<<8 | uint16(lo)
}

// write16 writes a little-endian word to memory (two 3T cycles).
func (c *CPU) write16(addr uint16, v uint16) {
	c.write8(addr, byte(v))
	c.write8(addr+1, byte(v>>8))
}

// ioRead performs an I/O read M-cycle (4T including the automatically
// inserted wait state): IORQ|RD asserted on TW (machine wait states
// sampled there), data on the bus in the final T-state.
func (c *CPU) ioRead(port uint16) byte {
	c.tick(port, DataNone, 0)       // T1
	c.tick(port, DataNone, 0)       // T2
	c.tick(port, DataNone, IORQ|RD) // TW (+waits)
	v := c.bus.IORead(port)
	c.tick(port, int16(v), 0) // T3
	return v
}

// ioWrite performs an I/O write M-cycle (4T including the automatic
// wait state): IORQ|WR asserted on TW with the data on the bus.
func (c *CPU) ioWrite(port uint16, v byte) {
	c.tick(port, DataNone, 0)       // T1
	c.tick(port, DataNone, 0)       // T2
	c.tick(port, int16(v), IORQ|WR) // TW (+waits)
	c.bus.IOWrite(port, v)
	c.tick(port, DataNone, 0) // T4
}

// internal emits n internal T-states with addr on the address bus and
// no control pins active (register-to-register work, 16-bit add delay,
// and similar).
func (c *CPU) internal(addr uint16, n int) {
	for ; n > 0; n-- {
		c.tick(addr, DataNone, 0)
	}
}

// push writes a word to the stack, high byte first, decrementing SP.
func (c *CPU) push(v uint16) {
	c.sp--
	c.write8(c.sp, byte(v>>8))
	c.sp--
	c.write8(c.sp, byte(v))
}

// pop reads a word from the stack, low byte first, incrementing SP.
func (c *CPU) pop() uint16 {
	lo := c.read8(c.sp)
	c.sp++
	hi := c.read8(c.sp)
	c.sp++
	return uint16(hi)<<8 | uint16(lo)
}

// ---------------------------------------------------------------------
// Interrupt acceptance.
// ---------------------------------------------------------------------

// intAck returns the byte the interrupting device drives on the data
// bus during an acknowledge cycle (0xFF if the machine supplies none).
func (c *CPU) intAck() byte {
	if c.acker != nil {
		return c.acker.IntAck()
	}
	return 0xFF
}

// intAckM1 emits the interrupt-acknowledge M-cycle (6T: the M1 shape
// with two automatically inserted wait states before IORQ, then the
// usual refresh pair). The device drives the data bus (via IntAck); R
// is incremented, PC is not. Totals: IM1 = 6+1+6 = 13T, IM2 =
// 6+1+6+6 = 19T, IM0 executing RST 38h = 6+7 = 13T.
func (c *CPU) intAckM1() byte {
	c.tick(c.pc, DataNone, M1)      // T1
	c.tick(c.pc, DataNone, M1)      // TW1
	c.tick(c.pc, DataNone, M1|IORQ) // TW2 (+waits)
	vec := c.intAck()
	c.tick(c.pc, int16(vec), M1) // T2 (data latched)
	ir := uint16(c.i)<<8 | uint16(c.r)
	c.lastIR = ir
	c.r = c.r&0x80 | (c.r+1)&0x7F
	c.tick(ir, DataNone, RFSH) // T3
	c.tick(ir, DataNone, RFSH) // T4
	return vec
}

// acceptNMI performs the non-maskable interrupt sequence (11T):
// a discarded opcode fetch with one extra internal T-state, then a push
// of PC and a jump to 0x0066. IFF1 is cleared, IFF2 preserved.
func (c *CPU) acceptNMI() {
	c.halted = false
	c.iff1 = false
	// M1: a normal-shape opcode fetch at PC (4T) with the data
	// discarded and PC not advanced, plus one internal T-state.
	pc := c.pc
	c.fetchM1(M1)
	c.pc = pc
	c.internal(c.irAddr(), 1)
	c.push(c.pc)
	c.pc = 0x0066
	c.wz = 0x0066
	c.q = 0
}

// acceptINT performs the maskable interrupt sequence for the current
// interrupt mode: IM 1 → RST 38h (13T); IM 2 → vector table call
// (19T); IM 0 → the instruction the device drives on the bus is
// executed, with all its bytes (prefixes and operands included)
// supplied by [IntAcker.IntAck] and PC left untouched. The default
// floating-bus value 0xFF makes IM 0 execute RST 38h.
func (c *CPU) acceptINT() {
	c.halted = false
	c.iff1, c.iff2 = false, false

	vec := c.intAckM1()

	switch c.im {
	case 2:
		c.internal(c.irAddr(), 1)
		c.push(c.pc)
		addr := uint16(c.i)<<8 | uint16(vec)
		c.pc = c.read16(addr)
		c.wz = c.pc
	case 1:
		c.internal(c.pc, 1)
		c.push(c.pc)
		c.pc = 0x0038
		c.wz = 0x0038
	default: // IM 0: execute the instruction the device puts on the bus.
		c.flagsSet = false
		c.idx, c.dispValid = modeHL, false
		c.im0 = true
		c.exec(vec)
		c.im0 = false
	}
	c.q = 0
}
