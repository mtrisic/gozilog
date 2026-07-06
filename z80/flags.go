package z80

// Flag bits of the F register. X and Y are the undocumented copies of
// result bits 3 and 5.
const (
	FlagC  byte = 0x01 // carry
	FlagN  byte = 0x02 // add/subtract
	FlagPV byte = 0x04 // parity/overflow
	FlagX  byte = 0x08 // undocumented: result bit 3
	FlagH  byte = 0x10 // half carry
	FlagY  byte = 0x20 // undocumented: result bit 5
	FlagZ  byte = 0x40 // zero
	FlagS  byte = 0x80 // sign
)

// szxy[v] holds the S, Z, X and Y flags for an 8-bit result v.
var szxy [256]byte

// parity[v] holds FlagPV if v has even parity, else 0.
var parity [256]byte

func init() {
	for i := 0; i < 256; i++ {
		v := byte(i)
		f := v & (FlagS | FlagX | FlagY)
		if v == 0 {
			f |= FlagZ
		}
		szxy[i] = f

		ones := 0
		for b := v; b != 0; b >>= 1 {
			ones += int(b & 1)
		}
		if ones%2 == 0 {
			parity[i] = FlagPV
		}
	}
}

// setF stores a fully computed flag byte and marks flags as modified by
// the current instruction (feeding the Q latch).
func (c *CPU) setF(f byte) {
	c.f = f
	c.flagsSet = true
}

// addFlags computes A+v (+carry) and sets all flags; returns the result.
func (c *CPU) add8(v byte, carry bool) byte {
	cin := byte(0)
	if carry {
		cin = 1
	}
	a := c.a
	r16 := uint16(a) + uint16(v) + uint16(cin)
	r := byte(r16)
	f := szxy[r]
	if r16 > 0xFF {
		f |= FlagC
	}
	if (a^v^r)&0x10 != 0 {
		f |= FlagH
	}
	if (a^r)&(v^r)&0x80 != 0 {
		f |= FlagPV
	}
	c.setF(f)
	return r
}

// sub8 computes A-v (-carry) and sets all flags; returns the result.
// Used by SUB, SBC and (with the result discarded and X/Y taken from
// the operand) CP.
func (c *CPU) sub8(v byte, carry bool) byte {
	cin := byte(0)
	if carry {
		cin = 1
	}
	a := c.a
	r16 := uint16(a) - uint16(v) - uint16(cin)
	r := byte(r16)
	f := szxy[r] | FlagN
	if r16 > 0xFF { // borrow
		f |= FlagC
	}
	if (a^v^r)&0x10 != 0 {
		f |= FlagH
	}
	if (a^v)&(a^r)&0x80 != 0 {
		f |= FlagPV
	}
	c.setF(f)
	return r
}

// and8/xor8/or8 implement the logic operations on A with their flag
// rules (S/Z/X/Y from result, PV = parity, C = N = 0; H = 1 only for
// AND).
func (c *CPU) and8(v byte) {
	c.a &= v
	c.setF(szxy[c.a] | parity[c.a] | FlagH)
}

func (c *CPU) xor8(v byte) {
	c.a ^= v
	c.setF(szxy[c.a] | parity[c.a])
}

func (c *CPU) or8(v byte) {
	c.a |= v
	c.setF(szxy[c.a] | parity[c.a])
}

// add16 implements ADD HL,v (and its IX/IY forms): S, Z and PV are
// preserved, H is the carry out of bit 11, X/Y come from the high byte
// of the result, and WZ is set to hl+1.
func (c *CPU) add16(hl, v uint16) uint16 {
	c.wz = hl + 1
	r32 := uint32(hl) + uint32(v)
	r := uint16(r32)
	f := c.f & (FlagS | FlagZ | FlagPV)
	f |= byte(r>>8) & (FlagX | FlagY)
	if r32 > 0xFFFF {
		f |= FlagC
	}
	if (hl^v^r)&0x1000 != 0 {
		f |= FlagH
	}
	c.setF(f)
	return r
}

// daa implements the decimal-adjust instruction, including the
// undocumented H behavior (from "The Undocumented Z80 Documented").
func (c *CPU) daa() {
	a, f := c.a, c.f
	var adjust byte
	newF := f & FlagN
	if f&FlagC != 0 || a > 0x99 {
		adjust = 0x60
		newF |= FlagC
	}
	if f&FlagH != 0 || a&0x0F > 9 {
		adjust |= 0x06
	}
	var r byte
	if f&FlagN != 0 {
		r = a - adjust
		if f&FlagH != 0 && a&0x0F < 6 {
			newF |= FlagH
		}
	} else {
		r = a + adjust
		if a&0x0F > 9 {
			newF |= FlagH
		}
	}
	c.a = r
	c.setF(newF | szxy[r] | parity[r])
}

// scfCcfXY computes the undocumented X/Y result of SCF and CCF, which
// depends on whether the previous instruction modified flags: the
// bits are ((Q ^ F) | A) & 0x28.
func (c *CPU) scfCcfXY() byte {
	return ((c.q ^ c.f) | c.a) & (FlagX | FlagY)
}

// incFlags sets the flags for INC r (result v, carry preserved).
func (c *CPU) incFlags(v byte) {
	f := szxy[v] | c.f&FlagC
	if v&0x0F == 0 {
		f |= FlagH
	}
	if v == 0x80 {
		f |= FlagPV
	}
	c.setF(f)
}

// decFlags sets the flags for DEC r (result v, carry preserved).
func (c *CPU) decFlags(v byte) {
	f := szxy[v] | c.f&FlagC | FlagN
	if v&0x0F == 0x0F {
		f |= FlagH
	}
	if v == 0x7F {
		f |= FlagPV
	}
	c.setF(f)
}
