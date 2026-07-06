package z80

// DD/FD prefix support. The prefixes do not get their own opcode
// tables: they set an index mode that the base-table handlers consume
// through the accessors below — HL-as-pair becomes IX/IY, H/L become
// IXH/IXL (except when the instruction addresses (HL), which becomes
// (IX+d) while H/L stay themselves), and (HL) becomes (IX+d) with a
// once-per-instruction displacement fetch.

type idxMode byte

const (
	modeHL idxMode = iota
	modeIX
	modeIY
)

func init() {
	implementedIdx = true
}

// hlIdx returns the active HL-role pair: HL, IX or IY.
func (c *CPU) hlIdx() uint16 {
	switch c.idx {
	case modeIX:
		return uint16(c.ixh)<<8 | uint16(c.ixl)
	case modeIY:
		return uint16(c.iyh)<<8 | uint16(c.iyl)
	default:
		return c.hl()
	}
}

// setHLIdx writes the active HL-role pair.
func (c *CPU) setHLIdx(v uint16) {
	switch c.idx {
	case modeIX:
		c.ixh, c.ixl = byte(v>>8), byte(v)
	case modeIY:
		c.iyh, c.iyl = byte(v>>8), byte(v)
	default:
		c.setHL(v)
	}
}

// eaHL resolves the (HL) operand: plain HL, or IX/IY plus a
// displacement fetched once per instruction (3T fetch + 5 internal
// T-states), which also sets WZ.
func (c *CPU) eaHL() uint16 {
	if c.idx == modeHL {
		return c.hl()
	}
	if !c.dispValid {
		d := c.fetch8()
		c.internal(c.pc-1, 5)
		c.dispEA = c.hlIdx() + uint16(int16(int8(d)))
		c.dispValid = true
		c.wz = c.dispEA
	}
	return c.dispEA
}

// getRPlain/setRPlain access register code r without index remapping —
// the H/L operands of instructions that also address (IX+d), and the
// register copy of DDCB results, always mean the real H and L.
func (c *CPU) getRPlain(code byte) byte {
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
		return c.h
	case 5:
		return c.l
	default:
		return c.a
	}
}

func (c *CPU) setRPlain(code byte, v byte) {
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
		c.h = v
	case 5:
		c.l = v
	default:
		c.a = v
	}
}

// execIdxCB executes a DDCB/FDCB-prefixed opcode: displacement first,
// then the final opcode byte — read as data, without refresh or R
// increment — then a read-modify-write on (IX+d). All operations,
// including the nominally register-only encodings, act on memory; the
// undocumented non-BIT encodings with z != 6 also copy the result to
// the (unremapped) register z.
func (c *CPU) execIdxCB() {
	d := c.fetch8()
	ea := c.hlIdx() + uint16(int16(int8(d)))
	c.wz = ea
	op := c.read8(c.pc)
	c.pc++
	c.internal(c.pc-1, 2)

	x, y, z := op>>6, op>>3&7, op&7
	v := c.read8(ea)
	c.internal(ea, 1)
	switch x {
	case 1: // BIT y,(IX+d): no write, X/Y from high byte of EA (== WZ)
		c.bitFlags(y, v, byte(ea>>8))
		return
	case 0:
		v = c.rot(y, v)
	case 2:
		v &^= 1 << y
	default:
		v |= 1 << y
	}
	c.write8(ea, v)
	if z != 6 {
		c.setRPlain(z, v)
	}
}
