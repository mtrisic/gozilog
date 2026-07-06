package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHelloGolden assembles examples/hello_from_z80.asm with pasmo,
// runs it to HALT twice, and requires both dumps to be byte-identical
// to each other (determinism) and to the committed golden file. It
// skips only when pasmo is unavailable (i.e. outside the devcontainer).
func TestHelloGolden(t *testing.T) {
	pasmo, err := exec.LookPath("pasmo")
	if err != nil {
		t.Skip("pasmo not in PATH — run this inside the devcontainer")
	}

	root := "../.."
	bin := filepath.Join(t.TempDir(), "hello.bin")
	cmd := exec.Command(pasmo, filepath.Join(root, "examples/hello_from_z80.asm"), bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pasmo: %v\n%s", err, out)
	}
	program, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}

	runOnce := func() []byte {
		m, _, err := run(program, 0x8000, 0x8000, 1_000_000)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := dump(&buf, &m.mem); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	first, second := runOnce(), runOnce()
	if !bytes.Equal(first, second) {
		t.Fatal("two identical runs produced different RAM dumps")
	}

	golden, err := os.ReadFile(filepath.Join(root, "examples/hello_from_z80.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, golden) {
		t.Fatalf("RAM dump differs from examples/hello_from_z80.golden\ngot:\n%s\nwant:\n%s", first, golden)
	}
}
