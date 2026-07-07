// Functional test of the npm package: drives the public createZ80()
// API in Node exactly as a consumer would. Run: node test.js
import { createZ80 } from "./index.js";

// The hello_from_z80 program (see examples/): copies "HELLO FROM Z80"
// to 0x9000 with a LD/DJNZ loop, then HALTs. 572 T-states on real
// datasheet timing.
const HELLO = new Uint8Array([
  0x21, 0x0f, 0x80, 0x11, 0x00, 0x90, 0x06, 0x0e,
  0x7e, 0x12, 0x23, 0x13, 0x10, 0xfa, 0x76,
  ...new TextEncoder().encode("HELLO FROM Z80"),
]);

function assert(cond, msg) {
  if (!cond) {
    console.error("FAIL:", msg);
    process.exit(1);
  }
}

const cpu = await createZ80();
cpu.load(HELLO, 0x8000, 0x8000);
const steps = cpu.run(1_000_000);
let st = cpu.state();
assert(st.halted, "CPU should be halted");
assert(steps === 74, `74 steps expected, got ${steps}`);
assert(st.tstates === 572, `572 T-states expected, got ${st.tstates}`);
const msg = new TextDecoder().decode(cpu.mem(0x9000, 14));
assert(msg === "HELLO FROM Z80", `memory result: ${JSON.stringify(msg)}`);

// Single stepping + registers.
cpu.load(HELLO, 0x8000, 0x8000);
assert(cpu.step() === 10, "LD HL,nn takes 10 T-states");
st = cpu.state();
assert(st.hl === 0x800f && st.pc === 0x8003, "HL/PC after LD HL,nn");

// setState round-trip.
const snap = cpu.state();
cpu.step();
cpu.setState(snap);
const back = cpu.state();
for (const k of Object.keys(snap)) {
  if (k === "tstates") continue; // monotonic counter, not restored
  assert(String(back[k]) === String(snap[k]), `setState round-trip: ${k}`);
}

// Instances are independent.
const cpu2 = await createZ80();
cpu2.load(HELLO, 0x4000, 0x4000);
cpu2.run(1_000_000);
assert(cpu2.state().halted && !cpu.state().halted, "instances must not share state");
assert(cpu.mem(0x9000, 1)[0] !== 0x48 || cpu2.mem(0x5000, 1)[0] === 0x48,
  "instances must not share memory");

// write() + interrupts basics: IM 1 acceptance out of HALT.
const cpu3 = await createZ80();
cpu3.load(new Uint8Array([0xfb, 0x76]), 0x0100, 0x0100); // EI; HALT
cpu3.write(0x0038, new Uint8Array([0x76])); // HALT at the IM1 vector
cpu3.run(10);
assert(cpu3.state().halted, "should halt after EI;HALT");
cpu3.setINT(true);
cpu3.step(); // accept interrupt → PC 0x0038
assert(cpu3.state().pc === 0x0038, `IM1 vector expected, pc=${cpu3.state().pc.toString(16)}`);

console.log("gozilog npm package: all tests OK");
