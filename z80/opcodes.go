package z80

import "fmt"

// baseOps dispatches unprefixed opcodes. Entries are installed in
// init(); a nil entry is an opcode not yet implemented and panics at
// dispatch — loudly incomplete rather than silently wrong. Phase 2
// fills the table completely (and adds the CB/ED prefix tables and
// DD/FD index-register remapping).
var baseOps [256]func(*CPU)

// implementedBase reports which unprefixed opcodes have handlers,
// derived from baseOps in init(). The SingleStepTests harness uses it
// to run exactly the implemented set and report the rest as skipped.
var implementedBase [256]bool

// implementedED and implementedIdx report ED-page and DD/FD coverage
// to the SST harness; set true by the files that implement them.
var implementedED = false
var implementedIdx = false

// exec dispatches one fetched opcode, consuming any run of DD/FD
// prefixes (each costs one 4T M1 cycle; the last one wins) before the
// prefix pages and base table.
func (c *CPU) exec(op byte) {
	for {
		switch op {
		case 0xDD:
			c.idx = modeIX
			c.q = 0 // a prefix acts as a flag-untouching M1, clearing Q
		case 0xFD:
			c.idx = modeIY
			c.q = 0
		case 0xCB:
			c.execCB()
			return
		case 0xED:
			c.idx = modeHL // ED ignores DD/FD
			c.execED()
			return
		default:
			fn := baseOps[op]
			if fn == nil {
				panic(fmt.Sprintf("z80: opcode 0x%02X not implemented (pc after fetch: 0x%04X)", op, c.pc))
			}
			fn(c)
			return
		}
		op = c.fetchOpcode()
	}
}

// ---------------------------------------------------------------------
// Register-pair and register-code accessors.
// ---------------------------------------------------------------------

func (c *CPU) bc() uint16 { return uint16(c.b)<<8 | uint16(c.c) }
func (c *CPU) de() uint16 { return uint16(c.d)<<8 | uint16(c.e) }
func (c *CPU) hl() uint16 { return uint16(c.h)<<8 | uint16(c.l) }

func (c *CPU) setBC(v uint16) { c.b, c.c = byte(v>>8), byte(v) }
func (c *CPU) setDE(v uint16) { c.d, c.e = byte(v>>8), byte(v) }
func (c *CPU) setHL(v uint16) { c.h, c.l = byte(v>>8), byte(v) }

// getR reads register code r (0..7 = B,C,D,E,H,L,(HL),A), honoring the
// DD/FD index mode: codes 4/5 become IXH/IXL (IYH/IYL) and code 6
// becomes (IX+d) with its displacement fetch. Code 6 costs a memory
// read.
func (c *CPU) getR(code byte) byte {
	switch code {
	case 0:
		return c.b
	case 1:
		return c.c
	case 2:
		return c.d
	case 3:
		return c.e
	case 4:
		return byte(c.hlIdx() >> 8)
	case 5:
		return byte(c.hlIdx())
	case 6:
		return c.read8(c.eaHL())
	default:
		return c.a
	}
}

// setR writes register code r (0..7), honoring the DD/FD index mode as
// getR does. Code 6 costs a memory write.
func (c *CPU) setR(code byte, v byte) {
	switch code {
	case 0:
		c.b = v
	case 1:
		c.c = v
	case 2:
		c.d = v
	case 3:
		c.e = v
	case 4:
		c.setHLIdx(uint16(v)<<8 | c.hlIdx()&0x00FF)
	case 5:
		c.setHLIdx(c.hlIdx()&0xFF00 | uint16(v))
	case 6:
		c.write8(c.eaHL(), v)
	default:
		c.a = v
	}
}

// cond evaluates condition code 0..7: NZ, Z, NC, C, PO, PE, P, M.
func (c *CPU) cond(code byte) bool {
	switch code >> 1 {
	case 0:
		return c.f&FlagZ != 0 == (code&1 != 0)
	case 1:
		return c.f&FlagC != 0 == (code&1 != 0)
	case 2:
		return c.f&FlagPV != 0 == (code&1 != 0)
	default:
		return c.f&FlagS != 0 == (code&1 != 0)
	}
}

// irAddr returns the refresh address (I<<8|R, pre-increment) of the
// current instruction's most recent M1 cycle — the value instructions
// place on the address bus during internal T-states.
func (c *CPU) irAddr() uint16 { return c.lastIR }

// ---------------------------------------------------------------------
// Table construction.
// ---------------------------------------------------------------------

func init() {
	defer func() {
		for i, fn := range baseOps {
			implementedBase[i] = fn != nil
		}
	}()

	// 8-bit load group: LD r,r' / LD r,(HL) / LD (HL),r (0x40–0x7F,
	// except 0x76 = HALT). When a DD/FD prefix turns (HL) into (IX+d),
	// the register operand keeps meaning the real H/L.
	for op := 0x40; op <= 0x7F; op++ {
		if op == 0x76 {
			continue
		}
		dst, src := byte(op>>3)&7, byte(op)&7
		switch {
		case dst == 6:
			baseOps[op] = func(c *CPU) { c.write8(c.eaHL(), c.getRPlain(src)) }
		case src == 6:
			baseOps[op] = func(c *CPU) { c.setRPlain(dst, c.read8(c.eaHL())) }
		default:
			baseOps[op] = func(c *CPU) { c.setR(dst, c.getR(src)) }
		}
	}

	// LD r,n (7T; LD (HL),n 10T).
	for _, op := range []byte{0x06, 0x0E, 0x16, 0x1E, 0x26, 0x2E, 0x3E} {
		dst := op >> 3 & 7
		baseOps[op] = func(c *CPU) { c.setR(dst, c.fetch8()) }
	}
	// LD (HL),n — indexed form reads d before n and replaces the usual
	// five displacement T-states with two after the operand (19T total).
	baseOps[0x36] = func(c *CPU) {
		if c.idx == modeHL {
			c.write8(c.hl(), c.fetch8())
			return
		}
		d := c.fetch8()
		ea := c.hlIdx() + uint16(int16(int8(d)))
		n := c.fetch8()
		c.internal(c.pc-1, 2)
		c.wz = ea
		c.write8(ea, n)
	}

	// ALU blocks: ADD A,r (0x80–0x87), SUB r (0x90–0x97), CP r (0xB8–0xBF).
	for src := byte(0); src < 8; src++ {
		src := src
		baseOps[0x80+src] = func(c *CPU) { c.a = c.add8(c.getR(src), false) }
		baseOps[0x90+src] = func(c *CPU) { c.a = c.sub8(c.getR(src), false) }
		baseOps[0xB8+src] = func(c *CPU) { c.cp8(c.getR(src)) }
	}
	baseOps[0xC6] = func(c *CPU) { c.a = c.add8(c.fetch8(), false) }
	baseOps[0xD6] = func(c *CPU) { c.a = c.sub8(c.fetch8(), false) }
	baseOps[0xFE] = func(c *CPU) { c.cp8(c.fetch8()) }

	// INC r / DEC r (4T; (HL) forms 11T with one internal state).
	for code := byte(0); code < 8; code++ {
		code := code
		baseOps[code<<3|0x04] = func(c *CPU) { c.incDecR(code, +1) }
		baseOps[code<<3|0x05] = func(c *CPU) { c.incDecR(code, -1) }
	}

	baseOps[0x00] = func(c *CPU) {} // NOP

	// LD dd,nn (10T).
	baseOps[0x01] = func(c *CPU) { c.setBC(c.fetch16()) }
	baseOps[0x11] = func(c *CPU) { c.setDE(c.fetch16()) }
	baseOps[0x21] = func(c *CPU) { c.setHLIdx(c.fetch16()) }
	baseOps[0x31] = func(c *CPU) { c.sp = c.fetch16() }

	// Indirect accumulator loads/stores.
	baseOps[0x0A] = func(c *CPU) { c.ldAInd(c.bc()) }      // LD A,(BC) 7T
	baseOps[0x1A] = func(c *CPU) { c.ldAInd(c.de()) }      // LD A,(DE) 7T
	baseOps[0x02] = func(c *CPU) { c.ldIndA(c.bc()) }      // LD (BC),A 7T
	baseOps[0x12] = func(c *CPU) { c.ldIndA(c.de()) }      // LD (DE),A 7T
	baseOps[0x3A] = func(c *CPU) { c.ldAInd(c.fetch16()) } // LD A,(nn) 13T
	baseOps[0x32] = func(c *CPU) { c.ldIndA(c.fetch16()) } // LD (nn),A 13T

	// LD HL,(nn) / LD (nn),HL (16T).
	baseOps[0x2A] = func(c *CPU) {
		nn := c.fetch16()
		c.setHLIdx(c.read16(nn))
		c.wz = nn + 1
	}
	baseOps[0x22] = func(c *CPU) {
		nn := c.fetch16()
		c.write16(nn, c.hlIdx())
		c.wz = nn + 1
	}

	// 16-bit INC/DEC (6T: two internal states on the IR address).
	baseOps[0x03] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setBC(c.bc() + 1) }
	baseOps[0x13] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setDE(c.de() + 1) }
	baseOps[0x23] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setHLIdx(c.hlIdx() + 1) }
	baseOps[0x33] = func(c *CPU) { c.internal(c.irAddr(), 2); c.sp++ }
	baseOps[0x0B] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setBC(c.bc() - 1) }
	baseOps[0x1B] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setDE(c.de() - 1) }
	baseOps[0x2B] = func(c *CPU) { c.internal(c.irAddr(), 2); c.setHLIdx(c.hlIdx() - 1) }
	baseOps[0x3B] = func(c *CPU) { c.internal(c.irAddr(), 2); c.sp-- }

	// Jumps.
	baseOps[0xC3] = func(c *CPU) { // JP nn 10T
		nn := c.fetch16()
		c.pc = nn
		c.wz = nn
	}
	for code := byte(0); code < 8; code++ {
		code := code
		baseOps[0xC2+code<<3] = func(c *CPU) { // JP cc,nn 10T
			nn := c.fetch16()
			c.wz = nn
			if c.cond(code) {
				c.pc = nn
			}
		}
		baseOps[0xC4+code<<3] = func(c *CPU) { c.call(c.cond(code)) } // CALL cc,nn 17/10T
		baseOps[0xC0+code<<3] = func(c *CPU) {                        // RET cc 11/5T
			c.internal(c.irAddr(), 1)
			if c.cond(code) {
				c.pc = c.pop()
				c.wz = c.pc
			}
		}
	}
	baseOps[0x18] = func(c *CPU) { c.jr(true) }           // JR e 12T
	baseOps[0x20] = func(c *CPU) { c.jr(c.f&FlagZ == 0) } // JR NZ
	baseOps[0x28] = func(c *CPU) { c.jr(c.f&FlagZ != 0) } // JR Z
	baseOps[0x30] = func(c *CPU) { c.jr(c.f&FlagC == 0) } // JR NC
	baseOps[0x38] = func(c *CPU) { c.jr(c.f&FlagC != 0) } // JR C
	baseOps[0x10] = func(c *CPU) {                        // DJNZ e 13/8T
		c.internal(c.irAddr(), 1)
		c.b--
		c.jr(c.b != 0)
	}

	// Calls, returns, stack.
	baseOps[0xCD] = func(c *CPU) { c.call(true) } // CALL nn 17T
	baseOps[0xC9] = func(c *CPU) {                // RET 10T
		c.pc = c.pop()
		c.wz = c.pc
	}
	baseOps[0xC5] = func(c *CPU) { c.internal(c.irAddr(), 1); c.push(c.bc()) }
	baseOps[0xD5] = func(c *CPU) { c.internal(c.irAddr(), 1); c.push(c.de()) }
	baseOps[0xE5] = func(c *CPU) { c.internal(c.irAddr(), 1); c.push(c.hlIdx()) }
	baseOps[0xF5] = func(c *CPU) {
		c.internal(c.irAddr(), 1)
		c.push(uint16(c.a)<<8 | uint16(c.f))
	}
	baseOps[0xC1] = func(c *CPU) { c.setBC(c.pop()) }
	baseOps[0xD1] = func(c *CPU) { c.setDE(c.pop()) }
	baseOps[0xE1] = func(c *CPU) { c.setHLIdx(c.pop()) }
	baseOps[0xF1] = func(c *CPU) {
		v := c.pop()
		c.a, c.f = byte(v>>8), byte(v)
	}

	// RST p (11T) — needed by IM 0 injected bytes (0xFF = RST 38h).
	for code := byte(0); code < 8; code++ {
		target := uint16(code) * 8
		baseOps[0xC7+code<<3] = func(c *CPU) {
			c.internal(c.irAddr(), 1)
			c.push(c.pc)
			c.pc = target
			c.wz = target
		}
	}

	// HALT (4T): PC advances past the instruction; while halted the CPU
	// keeps fetching (and discarding) the byte at PC — see haltCycle.
	baseOps[0x76] = func(c *CPU) { c.halted = true }

	baseOps[0xF3] = func(c *CPU) { c.iff1, c.iff2 = false, false } // DI
	baseOps[0xFB] = func(c *CPU) {                                 // EI: interrupts recognized after the next instruction
		c.iff1, c.iff2 = true, true
		c.eiPending = true
	}

	// Accumulator rotates (4T): S/Z/PV preserved, X/Y from A, H=N=0.
	rotA := func(c *CPU, r byte, carry bool) {
		c.a = r
		f := c.f & (FlagS | FlagZ | FlagPV)
		f |= r & (FlagX | FlagY)
		if carry {
			f |= FlagC
		}
		c.setF(f)
	}
	baseOps[0x07] = func(c *CPU) { rotA(c, c.a<<1|c.a>>7, c.a&0x80 != 0) } // RLCA
	baseOps[0x0F] = func(c *CPU) { rotA(c, c.a>>1|c.a<<7, c.a&0x01 != 0) } // RRCA
	baseOps[0x17] = func(c *CPU) {                                         // RLA
		rotA(c, c.a<<1|c.f&FlagC, c.a&0x80 != 0)
	}
	baseOps[0x1F] = func(c *CPU) { // RRA
		rotA(c, c.a>>1|c.f&FlagC<<7, c.a&0x01 != 0)
	}

	baseOps[0x27] = func(c *CPU) { c.daa() } // DAA
	baseOps[0x2F] = func(c *CPU) {           // CPL
		c.a = ^c.a
		c.setF(c.f&(FlagS|FlagZ|FlagPV|FlagC) | c.a&(FlagX|FlagY) | FlagH | FlagN)
	}
	baseOps[0x37] = func(c *CPU) { // SCF
		c.setF(c.f&(FlagS|FlagZ|FlagPV) | c.scfCcfXY() | FlagC)
	}
	baseOps[0x3F] = func(c *CPU) { // CCF
		f := c.f&(FlagS|FlagZ|FlagPV) | c.scfCcfXY()
		if c.f&FlagC != 0 {
			f |= FlagH
		} else {
			f |= FlagC
		}
		c.setF(f)
	}

	// ADC/SBC/AND/XOR/OR register blocks and immediates.
	for src := byte(0); src < 8; src++ {
		src := src
		baseOps[0x88+src] = func(c *CPU) { c.a = c.add8(c.getR(src), c.f&FlagC != 0) }
		baseOps[0x98+src] = func(c *CPU) { c.a = c.sub8(c.getR(src), c.f&FlagC != 0) }
		baseOps[0xA0+src] = func(c *CPU) { c.and8(c.getR(src)) }
		baseOps[0xA8+src] = func(c *CPU) { c.xor8(c.getR(src)) }
		baseOps[0xB0+src] = func(c *CPU) { c.or8(c.getR(src)) }
	}
	baseOps[0xCE] = func(c *CPU) { c.a = c.add8(c.fetch8(), c.f&FlagC != 0) }
	baseOps[0xDE] = func(c *CPU) { c.a = c.sub8(c.fetch8(), c.f&FlagC != 0) }
	baseOps[0xE6] = func(c *CPU) { c.and8(c.fetch8()) }
	baseOps[0xEE] = func(c *CPU) { c.xor8(c.fetch8()) }
	baseOps[0xF6] = func(c *CPU) { c.or8(c.fetch8()) }

	// ADD HL,ss (11T: fetch + 7 internal on IR).
	baseOps[0x09] = func(c *CPU) { c.internal(c.irAddr(), 7); c.setHLIdx(c.add16(c.hlIdx(), c.bc())) }
	baseOps[0x19] = func(c *CPU) { c.internal(c.irAddr(), 7); c.setHLIdx(c.add16(c.hlIdx(), c.de())) }
	baseOps[0x29] = func(c *CPU) { c.internal(c.irAddr(), 7); v := c.hlIdx(); c.setHLIdx(c.add16(v, v)) }
	baseOps[0x39] = func(c *CPU) { c.internal(c.irAddr(), 7); c.setHLIdx(c.add16(c.hlIdx(), c.sp)) }

	// Exchange group.
	baseOps[0x08] = func(c *CPU) { // EX AF,AF'
		c.a, c.a2 = c.a2, c.a
		c.f, c.f2 = c.f2, c.f
	}
	baseOps[0xD9] = func(c *CPU) { // EXX
		c.b, c.b2 = c.b2, c.b
		c.c, c.c2 = c.c2, c.c
		c.d, c.d2 = c.d2, c.d
		c.e, c.e2 = c.e2, c.e
		c.h, c.h2 = c.h2, c.h
		c.l, c.l2 = c.l2, c.l
	}
	baseOps[0xEB] = func(c *CPU) { // EX DE,HL
		c.d, c.h = c.h, c.d
		c.e, c.l = c.l, c.e
	}
	baseOps[0xE3] = func(c *CPU) { // EX (SP),HL 19T
		lo := c.read8(c.sp)
		hi := c.read8(c.sp + 1)
		c.internal(c.sp+1, 1)
		old := c.hlIdx()
		c.write8(c.sp+1, byte(old>>8))
		c.write8(c.sp, byte(old))
		c.internal(c.sp, 2)
		c.setHLIdx(uint16(hi)<<8 | uint16(lo))
		c.wz = uint16(hi)<<8 | uint16(lo)
	}

	// I/O with immediate port: address bus carries A<<8|n.
	baseOps[0xD3] = func(c *CPU) { // OUT (n),A 11T
		port := uint16(c.a)<<8 | uint16(c.fetch8())
		c.ioWrite(port, c.a)
		c.wz = uint16(c.a)<<8 | (port+1)&0x00FF
	}
	baseOps[0xDB] = func(c *CPU) { // IN A,(n) 11T — no flags affected
		port := uint16(c.a)<<8 | uint16(c.fetch8())
		c.a = c.ioRead(port)
		c.wz = port + 1
	}

	baseOps[0xE9] = func(c *CPU) { c.pc = c.hlIdx() } // JP (HL) 4T
	baseOps[0xF9] = func(c *CPU) {                    // LD SP,HL 6T
		c.internal(c.irAddr(), 2)
		c.sp = c.hlIdx()
	}
}

// ---------------------------------------------------------------------
// Multi-step opcode bodies.
// ---------------------------------------------------------------------

// incDecR implements INC r / DEC r; the (HL) form takes one internal
// T-state between read and write.
func (c *CPU) incDecR(code byte, delta int) {
	if code == 6 {
		addr := c.eaHL()
		v := c.read8(addr)
		c.internal(addr, 1)
		v = byte(int(v) + delta)
		if delta > 0 {
			c.incFlags(v)
		} else {
			c.decFlags(v)
		}
		c.write8(addr, v)
		return
	}
	v := byte(int(c.getR(code)) + delta)
	c.setR(code, v)
	if delta > 0 {
		c.incFlags(v)
	} else {
		c.decFlags(v)
	}
}

// cp8 implements CP v: subtraction flags with the result discarded and
// the undocumented X/Y flags taken from the operand, not the result.
func (c *CPU) cp8(v byte) {
	c.sub8(v, false)
	c.setF(c.f&^(FlagX|FlagY) | v&(FlagX|FlagY))
}

// ldAInd implements LD A,(addr): WZ = addr+1.
func (c *CPU) ldAInd(addr uint16) {
	c.a = c.read8(addr)
	c.wz = addr + 1
}

// ldIndA implements LD (addr),A: WZ = A<<8 | low(addr+1).
func (c *CPU) ldIndA(addr uint16) {
	c.write8(addr, c.a)
	c.wz = uint16(c.a)<<8 | (addr+1)&0x00FF
}

// jr implements the relative-jump tail shared by JR, JR cc and DJNZ:
// fetch the displacement, and if taken spend five internal T-states
// and set PC and WZ to the destination.
func (c *CPU) jr(taken bool) {
	d := c.fetch8()
	if !taken {
		return
	}
	c.internal(c.pc-1, 5)
	c.pc += uint16(int16(int8(d)))
	c.wz = c.pc
}

// call implements CALL nn / CALL cc,nn: WZ = nn in both cases; if taken,
// one internal T-state on the high-operand address, then push of the
// return address.
func (c *CPU) call(taken bool) {
	nn := c.fetch16()
	c.wz = nn
	if !taken {
		return
	}
	c.internal(c.pc-1, 1)
	c.push(c.pc)
	c.pc = nn
}
