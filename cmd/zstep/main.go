// zstep is a terminal UI for stepping through Z80 code: it loads a raw
// binary into a flat 64K RAM machine and shows registers, decoded
// flags, and a live memory view while you single-step.
//
// Usage:
//
//	zstep [-org addr] [-entry addr] program.bin
//
// Keys: space/s step · r run 100 · H run to HALT · f follow PC on/off ·
// pgup/pgdn scroll memory · g cycle memory anchor (PC/HL/SP) · q quit.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mtrisic/gozilog/z80"
)

type machine struct {
	mem [65536]byte
}

func (m *machine) MemRead(addr uint16) byte     { return m.mem[addr] }
func (m *machine) MemWrite(addr uint16, v byte) { m.mem[addr] = v }
func (m *machine) IORead(port uint16) byte      { return 0xFF }
func (m *machine) IOWrite(port uint16, v byte)  {}

type anchor int

const (
	anchorPC anchor = iota
	anchorHL
	anchorSP
)

func (a anchor) String() string { return [...]string{"PC", "HL", "SP"}[a] }

type model struct {
	mach   *machine
	cpu    *z80.CPU
	org    uint16
	steps  uint64
	lastT  int
	follow bool
	anchor anchor
	memTop uint16 // top address of the memory view when not following
	status string
}

const memRows = 16

var (
	panel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	title  = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Faint(true)
	hipc   = lipgloss.NewStyle().Reverse(true)
	halted = lipgloss.NewStyle().Bold(true).Blink(true)
)

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	m.status = ""
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case " ", "s":
		m.step(1)
	case "r":
		m.step(100)
	case "H":
		for i := 0; i < 1_000_000 && !m.cpu.Halted(); i++ {
			m.lastT = m.cpu.Step()
			m.steps++
		}
		if !m.cpu.Halted() {
			m.status = "no HALT within 1M steps"
		}
	case "f":
		m.follow = !m.follow
		if !m.follow {
			m.memTop = m.viewTop()
		}
	case "g":
		m.anchor = (m.anchor + 1) % 3
		m.follow = true
	case "pgdown":
		m.follow = false
		m.memTop = m.viewTop() + 8*16
	case "pgup":
		m.follow = false
		m.memTop = m.viewTop() - 8*16
	}
	return m, nil
}

func (m *model) step(n int) {
	for i := 0; i < n; i++ {
		if m.cpu.Halted() {
			m.status = "CPU halted — q to quit"
			return
		}
		m.lastT = m.cpu.Step()
		m.steps++
	}
}

// viewTop returns the first address of the memory panel.
func (m *model) viewTop() uint16 {
	if !m.follow {
		return m.memTop &^ 0x000F
	}
	st := m.cpu.State()
	var a uint16
	switch m.anchor {
	case anchorHL:
		a = st.HL
	case anchorSP:
		a = st.SP
	default:
		a = st.PC
	}
	// Keep the anchor on the fourth row so context above is visible.
	return (a &^ 0x000F) - 3*16
}

func flagString(f byte) string {
	names := "SZYHXPNC"
	bits := []byte{0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01}
	var b strings.Builder
	for i, bit := range bits {
		if f&bit != 0 {
			b.WriteByte(names[i])
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func onOff(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func (m *model) registersView() string {
	s := m.cpu.State()
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", title.Render("Registers"))
	fmt.Fprintf(&b, "AF %04X   AF' %04X   F %s\n", s.AF, s.AF2, flagString(byte(s.AF)))
	fmt.Fprintf(&b, "BC %04X   BC' %04X\n", s.BC, s.BC2)
	fmt.Fprintf(&b, "DE %04X   DE' %04X\n", s.DE, s.DE2)
	fmt.Fprintf(&b, "HL %04X   HL' %04X\n", s.HL, s.HL2)
	fmt.Fprintf(&b, "IX %04X   IY  %04X\n", s.IX, s.IY)
	fmt.Fprintf(&b, "SP %04X   PC  %04X\n", s.SP, s.PC)
	fmt.Fprintf(&b, "WZ %04X   I %02X  R %02X\n", s.WZ, s.I, s.R)
	fmt.Fprintf(&b, "IM %d  IFF1 %s  IFF2 %s\n", s.IM, onOff(s.IFF1), onOff(s.IFF2))
	fmt.Fprintf(&b, "\n%s\n", dim.Render(fmt.Sprintf("steps %d   T-states %d", m.steps, m.cpu.Tstates())))
	fmt.Fprintf(&b, "%s", dim.Render(fmt.Sprintf("last instr %d T", m.lastT)))
	if m.cpu.Halted() {
		fmt.Fprintf(&b, "\n\n%s", halted.Render("HALTED"))
	}
	return panel.Render(b.String())
}

func (m *model) memoryView() string {
	st := m.cpu.State()
	top := m.viewTop()
	var b strings.Builder
	mode := "following " + m.anchor.String()
	if !m.follow {
		mode = "free scroll"
	}
	fmt.Fprintf(&b, "%s %s\n\n", title.Render("Memory"), dim.Render("("+mode+")"))
	for row := 0; row < memRows; row++ {
		base := top + uint16(row*16)
		fmt.Fprintf(&b, "%04X  ", base)
		for col := 0; col < 16; col++ {
			addr := base + uint16(col)
			cell := fmt.Sprintf("%02X", m.mach.mem[addr])
			if addr == st.PC {
				cell = hipc.Render(cell)
			}
			b.WriteString(cell)
			b.WriteByte(' ')
		}
		b.WriteString(" ")
		for col := 0; col < 16; col++ {
			ch := m.mach.mem[base+uint16(col)]
			if ch < 0x20 || ch > 0x7E {
				b.WriteByte('.')
			} else {
				b.WriteByte(ch)
			}
		}
		b.WriteByte('\n')
	}
	return panel.Render(strings.TrimRight(b.String(), "\n"))
}

func (m *model) View() string {
	help := dim.Render("space step · r run 100 · H run to HALT · g anchor PC/HL/SP · f follow · pgup/pgdn scroll · q quit")
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.registersView(), m.memoryView())
	out := body + "\n" + help
	if m.status != "" {
		out += "\n" + title.Render(m.status)
	}
	return out
}

func parseAddr(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 0, 16)
	return uint16(v), err
}

func main() {
	orgFlag := flag.String("org", "0x8000", "load address of the binary")
	entryFlag := flag.String("entry", "", "entry point (default: same as -org)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: zstep [-org addr] [-entry addr] program.bin")
		os.Exit(2)
	}
	org, err := parseAddr(*orgFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zstep: bad -org: %v\n", err)
		os.Exit(2)
	}
	entry := org
	if *entryFlag != "" {
		if entry, err = parseAddr(*entryFlag); err != nil {
			fmt.Fprintf(os.Stderr, "zstep: bad -entry: %v\n", err)
			os.Exit(2)
		}
	}
	program, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zstep: %v\n", err)
		os.Exit(1)
	}
	if int(org)+len(program) > 0x10000 {
		fmt.Fprintf(os.Stderr, "zstep: program does not fit in 64K at %#04x\n", org)
		os.Exit(1)
	}

	mach := &machine{}
	copy(mach.mem[org:], program)
	cpu := z80.New(mach)
	cpu.SetState(z80.State{PC: entry, SP: 0xFFFF})

	m := &model{mach: mach, cpu: cpu, org: org, follow: true}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zstep: %v\n", err)
		os.Exit(1)
	}
}
