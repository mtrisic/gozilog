package z80

// ED-prefixed page. Only 0x40–0x7F and the block operations 0xA0–0xBB
// are real instructions; every other ED opcode is an 8T no-op (two M1
// cycles). Flag rules for the block I/O instructions follow "The
// Undocumented Z80 Documented"; SingleStepTests asserts all of them.

func init() {
	implementedED = true
}

// execED decodes and executes one ED-prefixed opcode (the prefix byte
// has already been fetched).
func (c *CPU) execED() {
	op := c.fetchOpcode()
	switch {
	case op >= 0x40 && op <= 0x7F:
		c.execED4x(op)
	case op >= 0xA0 && op <= 0xBB && op&7 <= 3:
		c.execEDBlock(op)
	default:
		// Invalid ED opcode: 8T no-op.
	}
}

// execED4x handles the 0x40–0x7F quadrant: y = bits 5..3, z = bits 2..0.
func (c *CPU) execED4x(op byte) {
	y, z := op>>3&7, op&7
	switch z {
	case 0: // IN r,(C) — r=6 is the undocumented IN (C): flags only
		v := c.ioRead(c.bc())
		c.wz = c.bc() + 1
		if y != 6 {
			c.setR(y, v)
		}
		c.setF(c.f&FlagC | szxy[v] | parity[v])
	case 1: // OUT (C),r — r=6 is the undocumented OUT (C),0
		v := byte(0)
		if y != 6 {
			v = c.getR(y)
		}
		c.ioWrite(c.bc(), v)
		c.wz = c.bc() + 1
	case 2: // SBC HL,ss (y even) / ADC HL,ss (y odd), 15T
		c.internal(c.irAddr(), 7)
		ss := c.pair16(y >> 1)
		if y&1 == 0 {
			c.sbc16(ss)
		} else {
			c.adc16(ss)
		}
	case 3: // LD (nn),dd (y even) / LD dd,(nn) (y odd), 20T
		nn := c.fetch16()
		if y&1 == 0 {
			c.write16(nn, c.pair16(y>>1))
		} else {
			c.setPair16(y>>1, c.read16(nn))
		}
		c.wz = nn + 1
	case 4: // NEG (all eight copies)
		a := c.a
		c.a = 0
		c.a = c.sub8(a, false)
	case 5: // RETN (y != 1) / RETI (y == 1): identical behavior
		c.iff1 = c.iff2
		c.pc = c.pop()
		c.wz = c.pc
	case 6: // IM 0/1/2 (y&3: 0,1→IM0; 2→IM1; 3→IM2, mirrored)
		switch y & 3 {
		case 2:
			c.im = 1
		case 3:
			c.im = 2
		default:
			c.im = 0
		}
	default: // z == 7
		switch y {
		case 0: // LD I,A (9T)
			c.internal(c.irAddr(), 1)
			c.i = c.a
		case 1: // LD R,A (9T)
			c.internal(c.irAddr(), 1)
			c.r = c.a
		case 2: // LD A,I (9T)
			c.internal(c.irAddr(), 1)
			c.a = c.i
			c.ldAIRFlags()
		case 3: // LD A,R (9T)
			c.internal(c.irAddr(), 1)
			c.a = c.r
			c.ldAIRFlags()
		case 4: // RRD (18T)
			c.rrdRld(false)
		case 5: // RLD (18T)
			c.rrdRld(true)
		default: // 6, 7: invalid → no-op
		}
	}
}

// ldAIRFlags sets the flags for LD A,I / LD A,R (PV = IFF2, the
// interrupt-sensitive erratum tracked by the P latch).
func (c *CPU) ldAIRFlags() {
	f := c.f&FlagC | szxy[c.a]
	if c.iff2 {
		f |= FlagPV
	}
	c.setF(f)
	c.p = 1
}

// rrdRld implements RRD/RLD: a nibble rotation between A and (HL),
// 18T (4+4+3+4 internal+3), WZ = HL+1.
func (c *CPU) rrdRld(left bool) {
	addr := c.hl()
	v := c.read8(addr)
	c.internal(addr, 4)
	var newA, newV byte
	if left {
		newA = c.a&0xF0 | v>>4
		newV = v<<4 | c.a&0x0F
	} else {
		newA = c.a&0xF0 | v&0x0F
		newV = c.a<<4 | v>>4
	}
	c.a = newA
	c.write8(addr, newV)
	c.wz = addr + 1
	c.setF(c.f&FlagC | szxy[c.a] | parity[c.a])
}

// pair16/setPair16 access register pairs by code 0..3 = BC,DE,HL,SP
// (the dd encoding used by ED and the 16-bit load/inc groups).
func (c *CPU) pair16(code byte) uint16 {
	switch code {
	case 0:
		return c.bc()
	case 1:
		return c.de()
	case 2:
		return c.hl()
	default:
		return c.sp
	}
}

func (c *CPU) setPair16(code byte, v uint16) {
	switch code {
	case 0:
		c.setBC(v)
	case 1:
		c.setDE(v)
	case 2:
		c.setHL(v)
	default:
		c.sp = v
	}
}

// sbc16 implements SBC HL,v: full flag treatment (unlike ADD HL,ss),
// WZ = HL+1.
func (c *CPU) sbc16(v uint16) {
	hl := c.hl()
	c.wz = hl + 1
	cin := uint32(c.f & FlagC)
	r32 := uint32(hl) - uint32(v) - cin
	r := uint16(r32)
	f := FlagN | byte(r>>8)&(FlagS|FlagX|FlagY)
	if r == 0 {
		f |= FlagZ
	}
	if r32 > 0xFFFF {
		f |= FlagC
	}
	if (hl^v^r)&0x1000 != 0 {
		f |= FlagH
	}
	if (hl^v)&(hl^r)&0x8000 != 0 {
		f |= FlagPV
	}
	c.setHL(r)
	c.setF(f)
}

// adc16 implements ADC HL,v: full flag treatment, WZ = HL+1.
func (c *CPU) adc16(v uint16) {
	hl := c.hl()
	c.wz = hl + 1
	cin := uint32(c.f & FlagC)
	r32 := uint32(hl) + uint32(v) + cin
	r := uint16(r32)
	f := byte(r>>8) & (FlagS | FlagX | FlagY)
	if r == 0 {
		f |= FlagZ
	}
	if r32 > 0xFFFF {
		f |= FlagC
	}
	if (hl^v^r)&0x1000 != 0 {
		f |= FlagH
	}
	if (hl^r)&(v^r)&0x8000 != 0 {
		f |= FlagPV
	}
	c.setHL(r)
	c.setF(f)
}

// execEDBlock handles LDI/CPI/INI/OUTI and variants: op = 0xA0–0xBB,
// bit 3 selects decrement, bit 4 selects repeat, low 2 bits the kind.
func (c *CPU) execEDBlock(op byte) {
	dec := op&0x08 != 0
	repeat := op&0x10 != 0
	step := uint16(1)
	if dec {
		step = 0xFFFF // -1
	}
	switch op & 3 {
	case 0: // LDI/LDD/LDIR/LDDR (16T, +5 when repeating)
		v := c.read8(c.hl())
		dst := c.de()
		c.write8(dst, v)
		c.internal(dst, 2)
		c.setHL(c.hl() + step)
		c.setDE(c.de() + step)
		c.setBC(c.bc() - 1)
		n := c.a + v
		f := c.f & (FlagS | FlagZ | FlagC)
		f |= n & FlagX
		if n&0x02 != 0 {
			f |= FlagY
		}
		if c.bc() != 0 {
			f |= FlagPV
		}
		c.setF(f)
		if repeat && c.bc() != 0 {
			c.internal(dst, 5)
			c.pc -= 2
			c.wz = c.pc + 1
			c.repeatXY()
		}
	case 1: // CPI/CPD/CPIR/CPDR (16T, +5 when repeating)
		src := c.hl()
		v := c.read8(src)
		c.internal(src, 5)
		res := c.a - v
		halfBorrow := (c.a^v^res)&0x10 != 0
		c.setHL(c.hl() + step)
		c.setBC(c.bc() - 1)
		c.wz += step
		f := c.f&FlagC | FlagN | szxy[res]&(FlagS|FlagZ)
		n := res
		if halfBorrow {
			f |= FlagH
			n--
		}
		f |= n & FlagX
		if n&0x02 != 0 {
			f |= FlagY
		}
		if c.bc() != 0 {
			f |= FlagPV
		}
		c.setF(f)
		if repeat && c.bc() != 0 && res != 0 {
			c.internal(src, 5)
			c.pc -= 2
			c.wz = c.pc + 1
			c.repeatXY()
		}
	case 2: // INI/IND/INIR/INDR (16T, +5 when repeating)
		c.internal(c.irAddr(), 1)
		v := c.ioRead(c.bc())
		c.wz = c.bc() + step
		c.b--
		dst := c.hl()
		c.write8(dst, v)
		c.setHL(c.hl() + step)
		c.inOutBlockFlags(v, byte(uint16(c.c)+step)+v)
		if repeat && c.b != 0 {
			c.internal(dst, 5)
			c.pc -= 2
			c.wz = c.pc + 1
			c.repeatXY()
			c.repeatIOFlags()
		}
	default: // OUTI/OUTD/OTIR/OTDR (16T, +5 when repeating)
		c.internal(c.irAddr(), 1)
		v := c.read8(c.hl())
		c.b--
		c.ioWrite(c.bc(), v)
		c.setHL(c.hl() + step)
		c.wz = c.bc() + step
		c.inOutBlockFlags(v, c.l+v)
		if repeat && c.b != 0 {
			c.internal(c.bc(), 5)
			c.pc -= 2
			c.wz = c.pc + 1
			c.repeatXY()
			c.repeatIOFlags()
		}
	}
}

// repeatXY applies the undocumented flag behavior common to all
// repeating block instructions when the loop is taken: X and Y come
// from the high byte of the (decremented) PC.
func (c *CPU) repeatXY() {
	c.setF(c.f&^(FlagX|FlagY) | byte(c.pc>>8)&(FlagX|FlagY))
}

// repeatIOFlags applies the additional H/PV adjustment the repeating
// block I/O instructions exhibit when the loop is taken (interrupted-
// instruction behavior from modern hardware research; formula verified
// against all SingleStepTests ed b2/b3/ba/bb repeat cases).
//
// With B' the decremented B: when C is set, H is recomputed from the
// low nibble of B' (as if B' were about to be decremented/incremented
// per N), and PV flips by parity((B'∓1)&7)^1; when C is clear, PV
// flips by parity(B'&7)^1.
func (c *CPU) repeatIOFlags() {
	f := c.f
	if f&FlagC != 0 {
		var adj byte
		if f&FlagN != 0 {
			adj = c.b - 1
			f = f&^FlagH | ifFlag(c.b&0x0F == 0x00, FlagH)
		} else {
			adj = c.b + 1
			f = f&^FlagH | ifFlag(c.b&0x0F == 0x0F, FlagH)
		}
		f ^= parity[adj&7] ^ FlagPV
	} else {
		f ^= parity[c.b&7] ^ FlagPV
	}
	c.setF(f)
}

// ifFlag returns flag when cond is true, else 0.
func ifFlag(cond bool, flag byte) byte {
	if cond {
		return flag
	}
	return 0
}

// inOutBlockFlags sets the undocumented flag results shared by the
// block I/O instructions: S/Z/X/Y from the new B, N from bit 7 of the
// transferred value, H and C from the 8-bit overflow of k, PV from
// the parity of (k&7)^B.
func (c *CPU) inOutBlockFlags(v, k byte) {
	f := szxy[c.b]
	if v&0x80 != 0 {
		f |= FlagN
	}
	if k < v { // sum wrapped: carry
		f |= FlagH | FlagC
	}
	f |= parity[k&7^c.b] & FlagPV
	c.setF(f)
}
