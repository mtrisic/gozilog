package z80

// Bus is the interface an embedding machine must implement. It carries
// pure data and has zero timing responsibility: the CPU owns the T-state
// schedule and invokes each method at the exact T-state the real Z80
// samples or drives the data bus. A Bus backed by plain arrays is a
// complete, correct machine.
//
// A Bus implementation may additionally implement [Ticker] and/or
// [IntAcker]; the CPU detects those once in [New].
type Bus interface {
	// MemRead returns the byte at addr. Called once per memory read
	// M-cycle, at the T-state the CPU samples the data bus.
	MemRead(addr uint16) byte

	// MemWrite stores data at addr. Called once per memory write
	// M-cycle, at the T-state the CPU asserts /WR.
	MemWrite(addr uint16, data byte)

	// IORead returns the byte read from the given port. The full 16-bit
	// port address is supplied (the Z80 places BC — or A<<8|n for
	// IN A,(n) — on the address bus during I/O cycles).
	IORead(port uint16) byte

	// IOWrite writes data to the given port. As with IORead, the full
	// 16-bit port address is supplied.
	IOWrite(port uint16, data byte)
}

// Pins is a bit set describing the Z80 control bus during one T-state,
// as reported to [Ticker.Tick].
type Pins uint16

// Control bus pins. Names match the Z80 signal names (all active-high
// here; the physical pins are active-low).
const (
	// MREQ indicates a memory request: the address bus carries a valid
	// memory address.
	MREQ Pins = 1 << iota
	// IORQ indicates an I/O request (or, together with M1, an interrupt
	// acknowledge cycle).
	IORQ
	// RD indicates the CPU is reading the data bus.
	RD
	// WR indicates the CPU is driving the data bus with write data.
	WR
	// M1 indicates an opcode fetch (machine cycle one) — or, together
	// with IORQ, an interrupt acknowledge cycle.
	M1
	// RFSH indicates the refresh portion of an M1 cycle: the address bus
	// carries I<<8 | R. Machines that use CPU refresh for video
	// addressing (Galaksija, ZX80/81) or DRAM refresh observe this.
	RFSH
	// HALTP indicates the CPU is halted and executing internal NOPs.
	// (Named HALTP to avoid colliding with the HALT opcode's mnemonic
	// in documentation; it reports the state of the /HALT pin.)
	HALTP
)

// DataNone is the data argument passed to [Ticker.Tick] for T-states
// during which the data bus is not driven or sampled.
const DataNone int16 = -1

// Ticker is an optional interface a [Bus] may implement to observe every
// T-state of execution and to insert wait states.
//
// Tick is called exactly once per T-state, in execution order, with the
// state of the address bus, data bus, and control pins during that
// T-state. data is [DataNone] when the data bus is not valid, otherwise
// 0–255. The return value is the number of wait T-states to insert
// before the CPU proceeds — the mechanism for memory/IO contention
// (e.g. ZX Spectrum ULA). Each inserted wait state is itself reported
// by a further Tick call (with the same address and pins) whose return
// value is ignored. Return 0 for no waits.
//
// The CPU accounts for every T-state of every M-cycle: an instruction
// that the datasheet lists as 7 T-states produces exactly 7 Tick calls
// (plus any waits). SPEC.md documents the per-M-cycle schedule.
//
// Tick runs on the CPU's goroutine in the middle of [CPU.Step]. It may
// call [CPU.SetINT] and [CPU.NMI] — that is how machines derive
// interrupt timing from the T-state stream; see those methods for the
// effect timing — but it must not re-enter [CPU.Step] or [CPU.Run].
//
// If the Bus does not implement Ticker, the CPU only advances its
// internal T-state counter; there is no per-T-state overhead.
type Ticker interface {
	Tick(addr uint16, data int16, pins Pins) int
}

// IntAcker is an optional interface a [Bus] may implement to supply the
// data bus value(s) the CPU reads during a maskable interrupt
// acknowledge cycle.
//
// In IM 2 it is called once for the vector byte. In IM 0 it is called
// for each byte of the instruction the interrupting device places on
// the bus (e.g. three times for a CALL nn). In IM 1 the value is
// ignored.
//
// If the Bus does not implement IntAcker, the CPU reads 0xFF — a
// floating bus pulled high — which makes IM 0 execute RST 38h, the
// correct behavior for most machines with no vectoring hardware.
type IntAcker interface {
	IntAck() byte
}
