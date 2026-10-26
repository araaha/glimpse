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

func TestUnderlineColorsAreIndependentAndResetSeparately(t *testing.T) {
	for _, tt := range []struct {
		code string
		rgb  uint32
	}{
		{"58:5:196", 0xff0000}, {"58;5;196", 0xff0000},
		{"58:2:1:2:3", 0x010203}, {"58:2::1:2:3", 0x010203}, {"58:2:0:1:2:3", 0x010203},
		{"58;2;1;2;3", 0x010203}, {"58;5;0", 0}, {"58:2::255:255:255", 0xffffff},
	} {
		got := tokens(t, "\x1b[4;32m\x1b["+tt.code+"mU\x1b[59mD")
		if len(got) != 2 {
			t.Fatalf("%q visible tokens: %+v", tt.code, got)
		}
		if rgb, set := got[0].UnderlineColor.RGB(); !set || rgb != tt.rgb {
			t.Fatalf("%q underline = %06x, %v; want %06x", tt.code, rgb, set, tt.rgb)
		}
		if _, set := got[1].UnderlineColor.RGB(); set {
			t.Fatalf("%q underline color not reset", tt.code)
		}
		for _, token := range got {
			if token.Style&Underline == 0 {
				t.Fatal("color reset disabled underline")
			}
			if rgb, _ := token.Style.Foreground(); rgb != 0x00ff00 {
				t.Fatalf("underline color changed text foreground: %06x", rgb)
			}
		}
	}
	got := tokens(t, "\x1b[4;58;5;196mA\x1b[24mB\x1b[4mC\x1b[0mD")
	if got[1].Style&Underline != 0 || got[1].UnderlineColor == 0 || got[2].Style&Underline == 0 || got[2].UnderlineColor == 0 {
		t.Fatal("underline enable/disable lost independent color")
	}
	if got[3].Style != 0 || got[3].UnderlineColor != 0 {
		t.Fatal("full reset kept underline color")
	}
}

func TestColonColorsDoNotBecomeUnrelatedAttributes(t *testing.T) {
	for _, code := range []string{"58:5:999", "58:2::256:0:0", "58:2:1:1:2:3", "58:2::-1:2:3", "4:3"} {
		got := tokens(t, "\x1b[4;58;5;196m\x1b["+code+"mX")
		if len(got) != 1 || got[0].Style != Underline || got[0].UnderlineColor != Color(0xff0001) {
			t.Fatalf("invalid/unsupported %q changed style: %+v", code, got)
		}
	}
}
