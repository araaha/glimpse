package app

import (
	"fmt"
	"strconv"
	"strings"
)

// JumpTarget is the screen row where search matches and line jumps land, as in
// less -j. Line counts from the top (1) or, when negative, the bottom (-1);
// otherwise Fraction is a share of the body height (.5 is the middle) and is
// recomputed for each jump. The zero value is the top row.
type JumpTarget struct {
	Line     int
	Fraction float64
}

// ParseJumpTarget accepts "4", "-2", ".5", or "0.5". A fraction must be below 1.
func ParseJumpTarget(value string) (JumpTarget, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, ".") {
		fraction, err := strconv.ParseFloat(value, 64)
		if err != nil || fraction < 0 || fraction >= 1 {
			return JumpTarget{}, fmt.Errorf("jump-target fraction must be at least 0 and below 1, such as .5")
		}
		return JumpTarget{Fraction: fraction}, nil
	}
	line, err := strconv.Atoi(value)
	if err != nil || line == 0 {
		return JumpTarget{}, fmt.Errorf("jump-target must be a nonzero screen line (1 is the top, -1 the bottom) or a fraction such as .5")
	}
	return JumpTarget{Line: line}, nil
}

// Row returns the zero-based body row for a viewport of height rows.
func (j JumpTarget) Row(height int) int {
	row := int(j.Fraction * float64(height))
	switch {
	case j.Line > 0:
		row = j.Line - 1
	case j.Line < 0:
		row = height + j.Line
	}
	return min(max(0, row), max(0, height-1))
}

func (j JumpTarget) String() string {
	if j.Line != 0 {
		return strconv.Itoa(j.Line)
	}
	if j.Fraction == 0 {
		return "1"
	}
	return strings.TrimPrefix(strconv.FormatFloat(j.Fraction, 'f', -1, 64), "0")
}
