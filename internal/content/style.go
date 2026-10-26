// Package content decodes a bounded, safe subset of terminal-styled input.
// Decoded text never contains executable terminal control sequences.
package content

import (
	"strconv"
	"strings"
)

// Style packs RGB foreground/background plus attributes into a fixed-size index
// record. Zero is the terminal default, not black.
type Style uint64

const (
	Bold Style = 1 << (50 + iota)
	Dim
	Italic
	Underline
	Reverse
	Strike
)
const colorMask Style = (1 << 25) - 1

func (s Style) Foreground() (uint32, bool) {
	value := uint32(s & colorMask)
	return value - 1, value != 0
}
func (s Style) Background() (uint32, bool) {
	value := uint32((s >> 25) & colorMask)
	return value - 1, value != 0
}
func (s Style) color(rgb uint32, background bool) Style {
	shift := uint(0)
	if background {
		shift = 25
	}
	return s & ^(colorMask<<shift) | Style(rgb+1)<<shift
}

func palette(index int) uint32 {
	colors := [...]uint32{0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xc0c0c0, 0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff}
	if index < 16 {
		return colors[index]
	}
	if index >= 232 {
		v := uint32(8 + 10*(index-232))
		return v<<16 | v<<8 | v
	}
	index -= 16
	level := func(n int) uint32 {
		if n == 0 {
			return 0
		}
		return uint32(55 + 40*n)
	}
	return level(index/36)<<16 | level(index/6%6)<<8 | level(index%6)
}

func sgr(style Style, parameters string) Style {
	if parameters == "" {
		return 0
	}
	parts := strings.Split(parameters, ";")
	if len(parts) > 64 {
		return style
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return style
		}
		values[i] = n
	}
	for i := 0; i < len(values); i++ {
		value := values[i]
		switch {
		case value == 0:
			style = 0
		case value == 1:
			style |= Bold
		case value == 2:
			style |= Dim
		case value == 3:
			style |= Italic
		case value == 4:
			style |= Underline
		case value == 7:
			style |= Reverse
		case value == 9:
			style |= Strike
		case value == 22:
			style &^= Bold | Dim
		case value == 23:
			style &^= Italic
		case value == 24:
			style &^= Underline
		case value == 27:
			style &^= Reverse
		case value == 29:
			style &^= Strike
		case value == 39:
			style &^= colorMask
		case value == 49:
			style &^= colorMask << 25
		case value >= 30 && value <= 37:
			style = style.color(palette(value-30), false)
		case value >= 40 && value <= 47:
			style = style.color(palette(value-40), true)
		case value >= 90 && value <= 97:
			style = style.color(palette(value-90+8), false)
		case value >= 100 && value <= 107:
			style = style.color(palette(value-100+8), true)
		case value == 38 || value == 48:
			background := value == 48
			if i+2 < len(values) && values[i+1] == 5 {
				if values[i+2] <= 255 {
					style = style.color(palette(values[i+2]), background)
				}
				i += 2
			} else if i+4 < len(values) && values[i+1] == 2 {
				r, g, b := values[i+2], values[i+3], values[i+4]
				if r <= 255 && g <= 255 && b <= 255 {
					style = style.color(uint32(r<<16|g<<8|b), background)
				}
				i += 4
			} else {
				return style
			}
		}
	}
	return style
}
