package z80

// CB-prefixed page: rotates/shifts (including the undocumented SLL),
// BIT, RES, SET. The page is fully regular, so it is decoded
// arithmetically rather than via a table: op = x<<6 | y<<3 | z with
// x selecting the group, y the bit/rotation kind, z the register code.

// implementedCB reports CB-page coverage to the SST harness.
var implementedCB = true

// execCB decodes and executes one CB-prefixed opcode (the prefix byte
// has already been fetched). Under a DD/FD prefix the whole page turns
// into (IX+d) read-modify-write forms handled by execIdxCB.
func (c *CPU) execCB() {
	if c.idx != modeHL {
		c.execIdxCB()
		return
	}
	op := c.fetchOpcode()
	x, y, z := op>>6, op>>3&7, op&7
	switch x {
	case 0: // rotate/shift group: 8T register, 15T (HL)
		if z == 6 {
			addr := c.hl()
			v := c.read8(addr)
			c.internal(addr, 1)
			c.write8(addr, c.rot(y, v))
		} else {
			c.setR(z, c.rot(y, c.getR(z)))
		}
	case 1: // BIT y,r: 8T register, 12T (HL)
		if z == 6 {
			addr := c.hl()
			v := c.read8(addr)
			c.internal(addr, 1)
			// Undocumented: X/Y of BIT n,(HL) come from the high byte
			// of WZ, not from the operand.
			c.bitFlags(y, v, byte(c.wz>>8))
		} else {
			v := c.getR(z)
			c.bitFlags(y, v, v)
		}
	case 2: // RES y,r: 8T register, 15T (HL)
		c.setResBit(z, y, false)
	default: // SET y,r
		c.setResBit(z, y, true)
	}
}

// rot applies rotation/shift kind (0..7 = RLC RRC RL RR SLA SRA SLL
// SRL) to v and sets the flags (S/Z/X/Y from result, PV = parity,
// H = N = 0, C from the shifted-out bit).
func (c *CPU) rot(kind, v byte) byte {
	var r byte
	var carry bool
	switch kind {
	case 0: // RLC
		r, carry = v<<1|v>>7, v&0x80 != 0
	case 1: // RRC
		r, carry = v>>1|v<<7, v&0x01 != 0
	case 2: // RL
		r, carry = v<<1|c.f&FlagC, v&0x80 != 0
	case 3: // RR
		r, carry = v>>1|c.f&FlagC<<7, v&0x01 != 0
	case 4: // SLA
		r, carry = v<<1, v&0x80 != 0
	case 5: // SRA
		r, carry = v&0x80|v>>1, v&0x01 != 0
	case 6: // SLL (undocumented: shifts 1 into bit 0)
		r, carry = v<<1|1, v&0x80 != 0
	default: // SRL
		r, carry = v>>1, v&0x01 != 0
	}
	f := szxy[r] | parity[r]
	if carry {
		f |= FlagC
	}
	c.setF(f)
	return r
}

// bitFlags sets the flags for BIT bit,v. X/Y come from xySrc: the
// operand itself for register forms, the high byte of WZ for (HL), and
// the high byte of the effective address for (IX+d)/(IY+d).
func (c *CPU) bitFlags(bit, v, xySrc byte) {
	r := v & (1 << bit)
	f := c.f&FlagC | FlagH | xySrc&(FlagX|FlagY)
	if r == 0 {
		f |= FlagZ | FlagPV
	}
	f |= r & FlagS
	c.setF(f)
}

// setResBit implements SET/RES y on register code z.
func (c *CPU) setResBit(z, y byte, set bool) {
	mask := byte(1) << y
	if z == 6 {
		addr := c.hl()
		v := c.read8(addr)
		c.internal(addr, 1)
		if set {
			v |= mask
		} else {
			v &^= mask
		}
		c.write8(addr, v)
		return
	}
	v := c.getR(z)
	if set {
		v |= mask
	} else {
		v &^= mask
	}
	c.setR(z, v)
}
