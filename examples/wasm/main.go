//go:build js && wasm

// Command wasm exposes the gozilog Z80 core to a web page — the seed
// of a browser-based machine emulator. Build with examples/wasm/build.sh
// and serve with `go run ./serve`; see index.html for the UI.
//
// The page drives the emulator through functions registered on the JS
// global object:
//
//	z80Load(bytes, org, entry)  load a program into a fresh machine
//	z80Step()                   execute one instruction, return T-states
//	z80Run(maxSteps)            run until HALT or budget, return steps
//	z80State()                  all registers as a JS object
//	z80Mem(addr, len)           memory slice as a Uint8Array
package main

import (
	"syscall/js"

	"github.com/mtrisic/gozilog/z80"
)

// machine is the simplest embedding: flat 64K RAM, no I/O.
type machine struct {
	mem [65536]byte
}

func (m *machine) MemRead(addr uint16) byte     { return m.mem[addr] }
func (m *machine) MemWrite(addr uint16, v byte) { m.mem[addr] = v }
func (m *machine) IORead(port uint16) byte      { return 0xFF }
func (m *machine) IOWrite(port uint16, v byte)  {}

var (
	mach *machine
	cpu  *z80.CPU
)

func load(_ js.Value, args []js.Value) any {
	program := make([]byte, args[0].Length())
	js.CopyBytesToGo(program, args[0])
	org := uint16(args[1].Int())
	entry := uint16(args[2].Int())

	mach = &machine{}
	copy(mach.mem[org:], program)
	cpu = z80.New(mach)
	cpu.SetState(z80.State{PC: entry, SP: 0xFFFF})
	return nil
}

func step(js.Value, []js.Value) any {
	if cpu.Halted() {
		return 0
	}
	return cpu.Step()
}

func run(_ js.Value, args []js.Value) any {
	max := args[0].Int()
	steps := 0
	for ; steps < max && !cpu.Halted(); steps++ {
		cpu.Step()
	}
	return steps
}

func state(js.Value, []js.Value) any {
	s := cpu.State()
	return js.ValueOf(map[string]any{
		"af": int(s.AF), "bc": int(s.BC), "de": int(s.DE), "hl": int(s.HL),
		"af2": int(s.AF2), "bc2": int(s.BC2), "de2": int(s.DE2), "hl2": int(s.HL2),
		"ix": int(s.IX), "iy": int(s.IY), "sp": int(s.SP), "pc": int(s.PC),
		"wz": int(s.WZ), "i": int(s.I), "r": int(s.R), "im": int(s.IM),
		"iff1": s.IFF1, "iff2": s.IFF2, "halted": s.Halted,
		"tstates": int(cpu.Tstates()),
	})
}

func mem(_ js.Value, args []js.Value) any {
	addr, n := args[0].Int(), args[1].Int()
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		buf[i] = mach.mem[uint16(addr+i)]
	}
	dst := js.Global().Get("Uint8Array").New(n)
	js.CopyBytesToJS(dst, buf)
	return dst
}

func main() {
	g := js.Global()
	g.Set("z80Load", js.FuncOf(load))
	g.Set("z80Step", js.FuncOf(step))
	g.Set("z80Run", js.FuncOf(run))
	g.Set("z80State", js.FuncOf(state))
	g.Set("z80Mem", js.FuncOf(mem))
	g.Call("dispatchEvent", g.Get("Event").New("z80ready"))
	select {} // keep the Go runtime alive for the callbacks
}
