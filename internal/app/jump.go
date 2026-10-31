package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// JumpTarget is the screen position where search matches and line jumps land:
// 0 is the bottom, .5 the middle, and 1 the top. The row is recomputed for each
// jump. The zero value defaults to the top, unlike an explicitly parsed "0".
type JumpTarget struct {
	fraction float64
	set      bool
}

// ParseJumpTarget accepts a position from 0 to 1, inclusive.
func ParseJumpTarget(value string) (JumpTarget, error) {
	value = strings.TrimSpace(value)
	fraction, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(fraction) || math.IsInf(fraction, 0) || fraction < 0 || fraction > 1 {
		return JumpTarget{}, fmt.Errorf("jump-target must be between 0 and 1 (0 is the bottom, 1 the top)")
	}
	return JumpTarget{fraction: fraction, set: true}, nil
}

// Row returns the zero-based body row for a viewport of height rows.
func (j JumpTarget) Row(height int) int {
	if !j.set || height <= 1 {
		return 0
	}
	return int(math.Round((1 - j.fraction) * float64(height-1)))
}

func (j JumpTarget) String() string {
	if !j.set {
		return "1"
	}
	value := strconv.FormatFloat(j.fraction, 'f', -1, 64)
	if strings.Contains(value, ".") {
		return strings.TrimPrefix(value, "0")
	}
	return value
}
