package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtrisic/gozilog/z80"
)

// newTestModel loads the hello program's copy loop directly.
func newTestModel() *model {
	program := []byte{
		0x21, 0x0F, 0x80, // LD HL,0x800F
		0x11, 0x00, 0x90, // LD DE,0x9000
		0x06, 0x0E, //       LD B,14
		0x7E,       //       LD A,(HL)
		0x12,       //       LD (DE),A
		0x23,       //       INC HL
		0x13,       //       INC DE
		0x10, 0xFA, //       DJNZ -6
		0x76, //             HALT
		'H', 'E', 'L', 'L', 'O', ' ', 'F', 'R', 'O', 'M', ' ', 'Z', '8', '0',
	}
	mach := &machine{}
	copy(mach.mem[0x8000:], program)
	cpu := z80.New(mach)
	cpu.SetState(z80.State{PC: 0x8000, SP: 0xFFFF})
	return &model{mach: mach, cpu: cpu, org: 0x8000, follow: true}
}

func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestStepAdvancesAndRendersState(t *testing.T) {
	m := newTestModel()
	if !strings.Contains(m.View(), "PC  8000") {
		t.Fatalf("initial view missing PC:\n%s", m.View())
	}

	m.Update(key(" ")) // LD HL,0x800F
	view := m.View()
	if !strings.Contains(view, "HL 800F") || !strings.Contains(view, "PC  8003") {
		t.Fatalf("after one step, view missing HL/PC update:\n%s", view)
	}
	if !strings.Contains(view, "steps 1") || !strings.Contains(view, "last instr 10 T") {
		t.Fatalf("step/T-state counters wrong:\n%s", view)
	}
}

func TestRunToHaltCopiesMessage(t *testing.T) {
	m := newTestModel()
	m.Update(key("H"))
	if !m.cpu.Halted() {
		t.Fatal("H did not run to HALT")
	}
	if got := string(m.mach.mem[0x9000:0x900E]); got != "HELLO FROM Z80" {
		t.Fatalf("program result = %q", got)
	}
	if !strings.Contains(m.View(), "HALTED") {
		t.Fatal("view does not show HALTED")
	}
}

func TestQuitAndMemoryControls(t *testing.T) {
	m := newTestModel()
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q did not produce a quit command")
	}

	// Anchor cycling PC → HL → SP and free scroll must not panic and
	// must change the view mode line.
	m.Update(key("g"))
	if !strings.Contains(m.View(), "following HL") {
		t.Fatal("anchor did not switch to HL")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if !strings.Contains(m.View(), "free scroll") {
		t.Fatal("pgdown did not enter free scroll")
	}
}
