//go:build js && wasm

// The WebAssembly entry point of the gozilog npm package. Unlike the
// browser demo (examples/wasm), this is library-shaped: it registers a
// single constructor, __gozilog_new, that returns an independent CPU
// instance as a JS object of methods — no other globals, any number of
// CPUs per page. The JS wrapper (index.js) hides even that.
package main

import (
	"syscall/js"

	"github.com/mtrisic/gozilog/z80"
)

// machine is the flat-RAM embedding the npm package exposes: 64K of
// memory, I/O reads float high (0xFF), I/O writes are dropped.
type machine struct {
	mem [65536]byte
}

func (m *machine) MemRead(addr uint16) byte     { return m.mem[addr] }
func (m *machine) MemWrite(addr uint16, v byte) { m.mem[addr] = v }
func (m *machine) IORead(port uint16) byte      { return 0xFF }
func (m *machine) IOWrite(port uint16, v byte)  {}

func stateToJS(cpu *z80.CPU) js.Value {
	s := cpu.State()
	o := js.Global().Get("Object").New()
	o.Set("af", int(s.AF))
	o.Set("bc", int(s.BC))
	o.Set("de", int(s.DE))
	o.Set("hl", int(s.HL))
	o.Set("af2", int(s.AF2))
	o.Set("bc2", int(s.BC2))
	o.Set("de2", int(s.DE2))
	o.Set("hl2", int(s.HL2))
	o.Set("ix", int(s.IX))
	o.Set("iy", int(s.IY))
	o.Set("sp", int(s.SP))
	o.Set("pc", int(s.PC))
	o.Set("wz", int(s.WZ))
	o.Set("i", int(s.I))
	o.Set("r", int(s.R))
	o.Set("im", int(s.IM))
	o.Set("iff1", s.IFF1)
	o.Set("iff2", s.IFF2)
	o.Set("halted", s.Halted)
	o.Set("eiPending", s.EIPending)
	o.Set("q", int(s.Q))
	o.Set("p", int(s.P))
	o.Set("tstates", int(cpu.Tstates()))
	return o
}

func stateFromJS(v js.Value) z80.State {
	u16 := func(k string) uint16 { return uint16(v.Get(k).Int()) }
	u8 := func(k string) byte { return byte(v.Get(k).Int()) }
	b := func(k string) bool { return v.Get(k).Truthy() }
	return z80.State{
		AF: u16("af"), BC: u16("bc"), DE: u16("de"), HL: u16("hl"),
		AF2: u16("af2"), BC2: u16("bc2"), DE2: u16("de2"), HL2: u16("hl2"),
		IX: u16("ix"), IY: u16("iy"), SP: u16("sp"), PC: u16("pc"),
		WZ: u16("wz"), I: u8("i"), R: u8("r"), IM: u8("im"),
		IFF1: b("iff1"), IFF2: b("iff2"), Halted: b("halted"),
		EIPending: b("eiPending"), Q: u8("q"), P: u8("p"),
	}
}

// newCPU is the __gozilog_new constructor: it returns a JS object whose
// methods close over one machine + CPU pair.
func newCPU(js.Value, []js.Value) any {
	m := &machine{}
	cpu := z80.New(m)

	o := js.Global().Get("Object").New()
	o.Set("load", js.FuncOf(func(_ js.Value, a []js.Value) any {
		// load(bytes, org, entry): clear memory, copy the program,
		// PC=entry, SP=0xFFFF (matching zrun's convention).
		*m = machine{}
		org := a[1].Int()
		buf := make([]byte, a[0].Length())
		js.CopyBytesToGo(buf, a[0])
		copy(m.mem[org&0xFFFF:], buf)
		cpu.SetState(z80.State{PC: uint16(a[2].Int()), SP: 0xFFFF})
		return nil
	}))
	o.Set("step", js.FuncOf(func(js.Value, []js.Value) any {
		return cpu.Step()
	}))
	o.Set("run", js.FuncOf(func(_ js.Value, a []js.Value) any {
		max := a[0].Int()
		steps := 0
		for ; steps < max && !cpu.Halted(); steps++ {
			cpu.Step()
		}
		return steps
	}))
	o.Set("halted", js.FuncOf(func(js.Value, []js.Value) any {
		return cpu.Halted()
	}))
	o.Set("state", js.FuncOf(func(js.Value, []js.Value) any {
		return stateToJS(cpu)
	}))
	o.Set("setState", js.FuncOf(func(_ js.Value, a []js.Value) any {
		cpu.SetState(stateFromJS(a[0]))
		return nil
	}))
	o.Set("mem", js.FuncOf(func(_ js.Value, a []js.Value) any {
		addr, n := a[0].Int(), a[1].Int()
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = m.mem[uint16(addr+i)]
		}
		dst := js.Global().Get("Uint8Array").New(n)
		js.CopyBytesToJS(dst, buf)
		return dst
	}))
	o.Set("write", js.FuncOf(func(_ js.Value, a []js.Value) any {
		addr := a[0].Int()
		buf := make([]byte, a[1].Length())
		js.CopyBytesToGo(buf, a[1])
		for i, v := range buf {
			m.mem[uint16(addr+i)] = v
		}
		return nil
	}))
	o.Set("setINT", js.FuncOf(func(_ js.Value, a []js.Value) any {
		cpu.SetINT(a[0].Truthy())
		return nil
	}))
	o.Set("nmi", js.FuncOf(func(js.Value, []js.Value) any {
		cpu.NMI()
		return nil
	}))
	o.Set("reset", js.FuncOf(func(js.Value, []js.Value) any {
		cpu.Reset()
		return nil
	}))
	return o
}

func main() {
	js.Global().Set("__gozilog_new", js.FuncOf(newCPU))
	select {} // keep the runtime alive for the callbacks
}
