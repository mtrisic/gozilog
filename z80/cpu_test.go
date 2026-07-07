package z80

import "testing"

// testBus is a minimal 64K machine recording T-states via Ticker.
type testBus struct {
	mem   [65536]byte
	ticks []sstCycle
	waits map[int]int // tick index -> waits to request
}

func (b *testBus) MemRead(addr uint16) byte     { return b.mem[addr] }
func (b *testBus) MemWrite(addr uint16, v byte) { b.mem[addr] = v }
func (b *testBus) IORead(port uint16) byte      { return 0xFF }
func (b *testBus) IOWrite(port uint16, v byte)  {}
func (b *testBus) Tick(addr uint16, data int16, pins Pins) int {
	b.ticks = append(b.ticks, sstCycle{addr, data, pins})
	return b.waits[len(b.ticks)-1]
}

func TestHaltAndInterruptWake(t *testing.T) {
	b := &testBus{}
	b.mem[0x0000] = 0x76 // HALT
	b.mem[0x0038] = 0x00 // NOP at IM1 vector
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1})

	if used := c.Step(); used != 4 || !c.Halted() {
		t.Fatalf("HALT: used %d T-states, halted=%v", used, c.Halted())
	}
	if s := c.State(); s.PC != 1 {
		t.Fatalf("HALT: PC = %04x, want 0001", s.PC)
	}
	// While halted with interrupts disabled, the CPU executes 4T NOPs.
	if used := c.Step(); used != 4 || !c.Halted() {
		t.Fatalf("halt cycle: used %d, halted=%v", used, c.Halted())
	}

	// IM1 acceptance: leaves halt, pushes PC (past the HALT), 13T.
	c.SetState(State{PC: 1, SP: 0x8000, IM: 1, IFF1: true, IFF2: true, Halted: true})
	c.SetINT(true)
	if used := c.Step(); used != 13 {
		t.Fatalf("IM1 accept: used %d T-states, want 13", used)
	}
	s := c.State()
	if s.PC != 0x0038 || s.WZ != 0x0038 || s.Halted || s.IFF1 || s.IFF2 {
		t.Fatalf("IM1 accept: state %+v", s)
	}
	if got := uint16(b.mem[0x7FFF])<<8 | uint16(b.mem[0x7FFE]); got != 1 {
		t.Fatalf("IM1 accept: pushed %04x, want 0001", got)
	}
}

func TestNMI(t *testing.T) {
	b := &testBus{}
	c := New(b)
	c.SetState(State{PC: 0x1234, SP: 0x8000, IFF1: true, IFF2: true})
	c.NMI()
	if used := c.Step(); used != 11 {
		t.Fatalf("NMI: used %d T-states, want 11", used)
	}
	s := c.State()
	if s.PC != 0x0066 || s.IFF1 || !s.IFF2 {
		t.Fatalf("NMI: PC=%04x IFF1=%v IFF2=%v, want 0066 false true", s.PC, s.IFF1, s.IFF2)
	}
}

func TestEIDefersInterrupt(t *testing.T) {
	b := &testBus{}
	b.mem[0] = 0xFB // EI
	b.mem[1] = 0x00 // NOP
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1})
	c.SetINT(true)

	c.Step() // EI — INT must not be accepted before the next instruction
	if s := c.State(); s.PC != 1 || !s.EIPending {
		t.Fatalf("after EI: %+v", s)
	}
	c.Step() // NOP executes, still no acceptance
	if s := c.State(); s.PC != 2 {
		t.Fatalf("after NOP: PC=%04x, want 0002 (INT accepted too early)", s.PC)
	}
	c.Step() // now the interrupt
	if s := c.State(); s.PC != 0x0038 {
		t.Fatalf("INT not accepted after EI+1: PC=%04x", s.PC)
	}
}

func TestTickerTraceAndWaitStates(t *testing.T) {
	b := &testBus{waits: map[int]int{}}
	b.mem[0] = 0x3E // LD A,n (7T)
	b.mem[1] = 0x42
	c := New(b)
	c.SetState(State{PC: 0})

	if used := c.Step(); used != 7 || len(b.ticks) != 7 {
		t.Fatalf("LD A,n: used %d T-states, %d ticks, want 7/7", used, len(b.ticks))
	}
	// M1 shape: T1 address out (M1 only), T2 M1|MREQ|RD, T3/T4 refresh
	// with the opcode riding the data bus on T3.
	if b.ticks[0].pins != M1 || b.ticks[0].addr != 0 {
		t.Fatalf("T1 = %+v", b.ticks[0])
	}
	if b.ticks[1].pins != M1|MREQ|RD {
		t.Fatalf("T2 pins = %v", b.ticks[1].pins)
	}
	if b.ticks[2].pins != RFSH || b.ticks[2].data != 0x3E {
		t.Fatalf("T3 (refresh) = %+v", b.ticks[2])
	}
	if c.State().AF>>8 != 0x42 {
		t.Fatalf("A = %02x, want 42", byte(c.State().AF>>8))
	}

	// Wait states requested by the machine stretch the instruction and
	// are visible in both the tick stream and the T-state counter.
	b.ticks = nil
	b.waits[1] = 3 // 3 waits on the M1 T2
	c.SetState(State{PC: 0})
	if used := c.Step(); used != 10 || len(b.ticks) != 10 {
		t.Fatalf("LD A,n with 3 waits: used %d T-states, %d ticks, want 10/10", used, len(b.ticks))
	}
}

// ackBus supplies a scripted byte stream during INT acknowledge.
type ackBus struct {
	testBus
	ackBytes []byte
	ackIdx   int
}

func (b *ackBus) IntAck() byte {
	if b.ackIdx < len(b.ackBytes) {
		v := b.ackBytes[b.ackIdx]
		b.ackIdx++
		return v
	}
	return 0xFF
}

func TestIM0MultiByteInjection(t *testing.T) {
	// Device injects CALL 0x1234: all three bytes must come from
	// IntAck, PC must not advance past the interrupted address, and
	// the pushed return address is the interrupted PC.
	b := &ackBus{ackBytes: []byte{0xCD, 0x34, 0x12}}
	c := New(b)
	c.SetState(State{PC: 0x4000, SP: 0x8000, IM: 0, IFF1: true, IFF2: true})
	c.SetINT(true)

	used := c.Step()
	s := c.State()
	if s.PC != 0x1234 {
		t.Fatalf("PC = %04x, want 1234", s.PC)
	}
	if got := uint16(b.mem[0x7FFF])<<8 | uint16(b.mem[0x7FFE]); got != 0x4000 {
		t.Fatalf("pushed %04x, want 4000 (interrupted PC)", got)
	}
	if used != 19 { // ack 6 + operand reads 3+3 + internal 1 + push 6
		t.Fatalf("IM0 CALL nn: %d T-states, want 19", used)
	}
	if b.ackIdx != 3 {
		t.Fatalf("IntAck consumed %d bytes, want 3", b.ackIdx)
	}
}

func TestSpecialReset(t *testing.T) {
	b := &testBus{}
	b.mem[0] = 0x76 // HALT at 0
	c := New(b)
	c.SetState(State{PC: 0x1234, SP: 0x8000, I: 0x55, R: 0x21, IM: 2,
		IFF1: true, IFF2: true, AF: 0xABCD, Halted: true})
	c.SpecialReset()
	s := c.State()
	if s.PC != 0 || s.Halted {
		t.Fatalf("PC=%04x halted=%v, want 0 false", s.PC, s.Halted)
	}
	// Everything else preserved — unlike a full Reset.
	if s.I != 0x55 || s.R != 0x21 || s.IM != 2 || !s.IFF1 || !s.IFF2 || s.AF != 0xABCD {
		t.Fatalf("special reset clobbered preserved state: %+v", s)
	}
}

func TestSnapshotRoundTripAndDeterminism(t *testing.T) {
	program := []byte{
		0x21, 0x00, 0x90, // LD HL,0x9000
		0x3E, 0xA5, //       LD A,0xA5
		0x77,       //       LD (HL),A
		0x23,       //       INC HL
		0x3D,       //       DEC A
		0x20, 0xFB, //       JR NZ,-5
		0x76, //             HALT
	}
	run := func() ([65536]byte, State, uint64) {
		b := &testBus{}
		copy(b.mem[:], program)
		c := New(b)
		for !c.Halted() {
			c.Step()
		}
		return b.mem, c.State(), c.Tstates()
	}
	mem1, st1, ts1 := run()
	mem2, st2, ts2 := run()
	if mem1 != mem2 || st1 != st2 || ts1 != ts2 {
		t.Fatal("two identical runs diverged")
	}
	if mem1[0x9000] != 0xA5 || mem1[0x90A4] != 0x01 {
		t.Fatalf("program result wrong: %02x %02x", mem1[0x9000], mem1[0x90A4])
	}

	// Snapshot mid-run, restore into a fresh CPU, and confirm identical
	// completion.
	b := &testBus{}
	copy(b.mem[:], program)
	c := New(b)
	for i := 0; i < 10; i++ {
		c.Step()
	}
	snap := c.State()

	b2 := &testBus{mem: b.mem}
	c2 := New(b2)
	c2.SetState(snap)
	for !c.Halted() {
		c.Step()
		c2.Step()
	}
	if c.State() != c2.State() || b.mem != b2.mem {
		t.Fatal("restored CPU diverged from original")
	}
}

// hookBus is a testBus whose Ticker additionally invokes a callback at
// chosen T-state indexes — the harness for the documented guarantee
// that SetINT/NMI may be called from inside Tick.
type hookBus struct {
	testBus
	n     int
	hooks map[int]func()
}

func (b *hookBus) Tick(addr uint16, data int16, pins Pins) int {
	w := b.testBus.Tick(addr, data, pins)
	if fn := b.hooks[b.n]; fn != nil {
		fn()
	}
	b.n++
	return w
}

// newHookBus returns a hookBus with LD (0x9000),A — a 13T instruction
// (M1 4T, two operand reads 3T each, write 3T) — at PC 0.
func newHookBus() *hookBus {
	b := &hookBus{hooks: map[int]func(){}}
	b.mem[0] = 0x32 // LD (nn),A
	b.mem[1] = 0x00
	b.mem[2] = 0x90
	return b
}

func TestSetINTFromTicker(t *testing.T) {
	// A machine that raises /INT from inside Tick, mid-instruction
	// (T-state 6 of the 13T LD (nn),A). The instruction in flight must
	// complete untouched; acceptance happens on the next Step.
	b := newHookBus()
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1, IFF1: true, IFF2: true})
	b.hooks[6] = func() { c.SetINT(true) }

	if used := c.Step(); used != 13 {
		t.Fatalf("LD (nn),A: used %d T-states, want 13 (INT must not preempt)", used)
	}
	if s := c.State(); s.PC != 3 {
		t.Fatalf("after LD (nn),A: PC = %04x, want 0003", s.PC)
	}
	if used := c.Step(); used != 13 {
		t.Fatalf("IM1 accept: used %d T-states, want 13", used)
	}
	if s := c.State(); s.PC != 0x0038 {
		t.Fatalf("IM1 accept: PC = %04x, want 0038", s.PC)
	}
	if got := uint16(b.mem[0x7FFF])<<8 | uint16(b.mem[0x7FFE]); got != 3 {
		t.Fatalf("IM1 accept: pushed %04x, want 0003", got)
	}
}

func TestINTPulseWithinInstructionInvisible(t *testing.T) {
	// Assert on T-state 5 and deassert on T-state 9 of the same
	// instruction: the line is sampled at instruction boundaries only,
	// so the pulse must never be accepted.
	b := newHookBus()
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1, IFF1: true, IFF2: true})
	b.hooks[5] = func() { c.SetINT(true) }
	b.hooks[9] = func() { c.SetINT(false) }

	c.Step() // LD (nn),A, with the pulse inside
	for i := 0; i < 2; i++ {
		c.Step() // NOPs at 3, 4
	}
	if s := c.State(); s.PC != 5 {
		t.Fatalf("PC = %04x, want 0005 (a within-instruction INT pulse was accepted)", s.PC)
	}
}

func TestNMIFromTicker(t *testing.T) {
	// NMI() from inside Tick mid-instruction, with /INT asserted at the
	// same moment: the edge is accepted exactly once at the next
	// boundary and wins over the maskable interrupt.
	b := newHookBus()
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1, IFF1: true, IFF2: true})
	b.hooks[6] = func() { c.NMI(); c.SetINT(true) }

	if used := c.Step(); used != 13 {
		t.Fatalf("LD (nn),A: used %d T-states, want 13 (NMI must not preempt)", used)
	}
	if used := c.Step(); used != 11 {
		t.Fatalf("NMI accept: used %d T-states, want 11", used)
	}
	s := c.State()
	if s.PC != 0x0066 || s.IFF1 || !s.IFF2 {
		t.Fatalf("NMI accept: PC=%04x IFF1=%v IFF2=%v, want 0066 false true", s.PC, s.IFF1, s.IFF2)
	}
	if got := uint16(b.mem[0x7FFF])<<8 | uint16(b.mem[0x7FFE]); got != 3 {
		t.Fatalf("NMI accept: pushed %04x, want 0003", got)
	}
	c.Step() // NOP at 0x0066 — no second acceptance (IFF1 now clear)
	if s := c.State(); s.PC != 0x0067 {
		t.Fatalf("after NMI handler NOP: PC = %04x, want 0067 (edge accepted twice?)", s.PC)
	}
}

func TestSetINTFromTickerWithWaits(t *testing.T) {
	// The Ticker requests wait states on the very T-state where it
	// raises /INT: the line change must survive the wait re-ticks
	// unchanged — neither lost nor applied early.
	b := newHookBus()
	b.waits = map[int]int{6: 2}
	c := New(b)
	c.SetState(State{PC: 0, SP: 0x8000, IM: 1, IFF1: true, IFF2: true})
	b.hooks[6] = func() { c.SetINT(true) }

	if used := c.Step(); used != 15 {
		t.Fatalf("LD (nn),A with 2 waits: used %d T-states, want 15", used)
	}
	if s := c.State(); s.PC != 3 {
		t.Fatalf("after LD (nn),A: PC = %04x, want 0003", s.PC)
	}
	if used := c.Step(); used != 13 {
		t.Fatalf("IM1 accept: used %d T-states, want 13", used)
	}
	if s := c.State(); s.PC != 0x0038 {
		t.Fatalf("IM1 accept: PC = %04x, want 0038", s.PC)
	}
	c.Step() // NOP at 0x0038 — IFF1 cleared, no re-acceptance
	if s := c.State(); s.PC != 0x0039 {
		t.Fatalf("after handler NOP: PC = %04x, want 0039", s.PC)
	}
}
