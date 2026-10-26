package content

import "testing"

func TestBasicForegroundsUseBrightColors(t *testing.T) {
	got := tokens(t, "\x1b[31mR\x1b[38;5;1mD\x1b[38;2;128;0;0mT\x1b[41mB")
	for i, want := range []uint32{0xff0000, 0x800000, 0x800000} {
		if rgb, set := got[i].Style.Foreground(); !set || rgb != want {
			t.Fatalf("glyph %d foreground = %06x, %v; want %06x", i, rgb, set, want)
		}
	}
	if rgb, set := got[3].Style.Background(); !set || rgb != 0x800000 {
		t.Fatalf("explicit background changed: %06x, %v", rgb, set)
	}
}
