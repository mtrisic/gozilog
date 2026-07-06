// zrun loads a raw Z80 binary into a flat 64K RAM machine, executes it
// until HALT, and writes a deterministic RAM dump to stdout. It exists
// to run the examples/ programs and to prove deterministic execution
// (two runs of the same binary must produce byte-identical dumps).
//
// Usage:
//
//	zrun [-org addr] [-entry addr] [-max tstates] program.bin
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/mtrisic/gozilog/z80"
)

// machine is the simplest possible embedding: 64K of RAM, no I/O.
type machine struct {
	mem [65536]byte
}

func (m *machine) MemRead(addr uint16) byte     { return m.mem[addr] }
func (m *machine) MemWrite(addr uint16, v byte) { m.mem[addr] = v }
func (m *machine) IORead(port uint16) byte      { return 0xFF }
func (m *machine) IOWrite(port uint16, v byte)  {}

// run loads the binary at org, jumps to entry and executes until HALT,
// returning the machine and the T-states consumed.
func run(program []byte, org, entry uint16, maxTstates uint64) (*machine, uint64, error) {
	if int(org)+len(program) > 0x10000 {
		return nil, 0, fmt.Errorf("program (%d bytes at %#04x) does not fit in 64K", len(program), org)
	}
	m := &machine{}
	copy(m.mem[org:], program)

	cpu := z80.New(m)
	cpu.SetState(z80.State{PC: entry, SP: 0xFFFF})
	for !cpu.Halted() {
		if cpu.Tstates() > maxTstates {
			return nil, 0, fmt.Errorf("no HALT after %d T-states", maxTstates)
		}
		cpu.Step()
	}
	return m, cpu.Tstates(), nil
}

// dump writes RAM as text hex, 16 bytes per line, skipping all-zero
// lines. The format is fixed: it is diffed against committed golden
// files.
func dump(w io.Writer, mem *[65536]byte) error {
	for base := 0; base < 0x10000; base += 16 {
		row := mem[base : base+16]
		zero := true
		for _, b := range row {
			if b != 0 {
				zero = false
				break
			}
		}
		if zero {
			continue
		}
		if _, err := fmt.Fprintf(w, "%04X:", base); err != nil {
			return err
		}
		for _, b := range row {
			if _, err := fmt.Fprintf(w, " %02X", b); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func parseAddr(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 0, 16)
	return uint16(v), err
}

func main() {
	orgFlag := flag.String("org", "0x8000", "load address of the binary")
	entryFlag := flag.String("entry", "", "entry point (default: same as -org)")
	maxFlag := flag.Uint64("max", 100_000_000, "abort if no HALT after this many T-states")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: zrun [-org addr] [-entry addr] [-max tstates] program.bin")
		os.Exit(2)
	}

	org, err := parseAddr(*orgFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zrun: bad -org: %v\n", err)
		os.Exit(2)
	}
	entry := org
	if *entryFlag != "" {
		if entry, err = parseAddr(*entryFlag); err != nil {
			fmt.Fprintf(os.Stderr, "zrun: bad -entry: %v\n", err)
			os.Exit(2)
		}
	}

	program, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zrun: %v\n", err)
		os.Exit(1)
	}

	m, tstates, err := run(program, org, entry, *maxFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zrun: %v\n", err)
		os.Exit(1)
	}
	if err := dump(os.Stdout, &m.mem); err != nil {
		fmt.Fprintf(os.Stderr, "zrun: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "halted after %d T-states\n", tstates)
}
