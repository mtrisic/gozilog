package disasm_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtrisic/gozilog/z80/disasm"
)

var update = flag.Bool("update", false, "rewrite testdata/opcodes.golden")

// generateMatrix disassembles the complete opcode space at address
// 0x0100 with 34 12 as the operand filler bytes and renders one line
// per pattern: hex bytes, two spaces, text.
func generateMatrix() string {
	single := func(pre ...byte) [][]byte {
		var ps [][]byte
		for i := 0; i < 256; i++ {
			ps = append(ps, append(append([]byte{}, pre...), byte(i)))
		}
		return ps
	}
	var sections []struct {
		name string
		pats [][]byte
	}
	add := func(name string, pats [][]byte) {
		sections = append(sections, struct {
			name string
			pats [][]byte
		}{name, pats})
	}
	add("unprefixed", single())
	add("CB", single(0xCB))
	add("ED", single(0xED))
	add("DD", single(0xDD))
	add("FD", single(0xFD))
	for _, pre := range []byte{0xDD, 0xFD} {
		for _, d := range []byte{0x00, 0x7F, 0x80} {
			add(fmt.Sprintf("%02X CB d=%02X", pre, d), single(pre, 0xCB, d))
		}
	}

	const addr = 0x0100
	filler := []byte{0x34, 0x12}
	var sb strings.Builder
	for _, sec := range sections {
		fmt.Fprintf(&sb, "# %s\n", sec.name)
		for _, p := range sec.pats {
			read := func(a uint16) byte {
				off := int(a - addr)
				if off < len(p) {
					return p[off]
				}
				return filler[(off-len(p))%len(filler)]
			}
			ins := disasm.Decode(read, addr)
			hex := make([]string, ins.Len)
			for i := 0; i < ins.Len; i++ {
				hex[i] = fmt.Sprintf("%02X", ins.Bytes[i])
			}
			fmt.Fprintf(&sb, "%-11s  %s\n", strings.Join(hex, " "), ins.Text)
		}
	}
	return sb.String()
}

// TestGoldenMatrix compares the disassembly of the complete opcode
// space against the committed golden file. Regenerate deliberately
// with: go test ./z80/disasm -run TestGoldenMatrix -update
func TestGoldenMatrix(t *testing.T) {
	got := generateMatrix()
	path := filepath.Join("testdata", "opcodes.golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (regenerate with -update): %v", err)
	}
	if got == string(want) {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		g, w := "", ""
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("golden mismatch at line %d:\n  got:  %q\n  want: %q", i+1, g, w)
		}
	}
}
