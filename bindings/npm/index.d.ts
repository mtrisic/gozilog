/** Complete CPU state. 16-bit register pairs, high byte first (af = A<<8|F). */
export interface Z80State {
  af: number; bc: number; de: number; hl: number;
  af2: number; bc2: number; de2: number; hl2: number;
  ix: number; iy: number; sp: number; pc: number;
  /** MEMPTR — the Z80's internal address latch. */
  wz: number;
  i: number; r: number; im: number;
  iff1: boolean; iff2: boolean;
  halted: boolean;
  /** True when the previous instruction was EI (interrupts deferred one instruction). */
  eiPending: boolean;
  /** Q latch (undocumented SCF/CCF X/Y behavior). */
  q: number;
  /** P latch (LD A,I / LD A,R erratum tracking). */
  p: number;
  /** Total T-states executed (read-only; ignored by setState). */
  tstates: number;
}

/** A Z80 CPU with 64K of flat RAM. I/O reads return 0xFF, writes are dropped. */
export interface Z80 {
  /** Clear memory, copy program at org, set PC=entry and SP=0xFFFF. */
  load(program: Uint8Array, org: number, entry: number): void;
  /** Execute one instruction; returns the T-states consumed. */
  step(): number;
  /** Execute up to maxSteps instructions, stopping at HALT; returns steps executed. */
  run(maxSteps: number): number;
  halted(): boolean;
  state(): Z80State;
  /** Restore a complete state (all fields required; see state()). */
  setState(state: Omit<Z80State, "tstates"> & { tstates?: number }): void;
  /** Copy len bytes of memory starting at addr (wraps at 64K). */
  mem(addr: number, len: number): Uint8Array;
  /** Write bytes into memory starting at addr (wraps at 64K). */
  write(addr: number, bytes: Uint8Array): void;
  /** Set the level of the maskable interrupt line. */
  setINT(active: boolean): void;
  /** Latch a non-maskable interrupt edge. */
  nmi(): void;
  /** Apply the /RESET sequence (PC/I/R/IM cleared, registers kept). */
  reset(): void;
}

/** Create an independent CPU instance (the WASM module is shared and lazily loaded). */
export function createZ80(): Promise<Z80>;
