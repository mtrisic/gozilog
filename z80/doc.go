// Package z80 implements a cycle-accurate emulator of the Zilog Z80 CPU.
//
// The package is machine-agnostic: it emulates only the CPU. An embedding
// machine supplies memory and I/O by implementing [Bus], and may observe
// every T-state of bus activity by additionally implementing [Ticker] —
// precise enough to build machines where the CPU participates in video
// generation (Galaksija, ZX80/81) or where memory access is contended
// (ZX Spectrum).
//
// Execution is deterministic: the emulator uses no wall clock, no
// goroutines, and no randomness. Identical initial state and bus behavior
// produce byte-identical results.
//
// The complete timing model is documented in SPEC.md at the repository
// root, and in the documentation of [Ticker].
package z80
