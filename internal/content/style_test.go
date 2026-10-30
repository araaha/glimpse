package content

import (
	"fmt"
	"strings"
	"testing"
)

func TestBasicForegroundsPreserveNormalPaletteColors(t *testing.T) {
	got := tokens(t, "\x1b[31mR\x1b[38;5;1mD\x1b[38;2;128;0;0mT\x1b[41mB")
	for i, want := range []uint32{0x800000, 0x800000, 0x800000} {
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
			if rgb, _ := token.Style.Foreground(); rgb != 0x008000 {
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
		if len(got) != 1 || got[0].Style != Underline || got[0].UnderlineColor != indexedColor(196) {
			t.Fatalf("invalid/unsupported %q changed style: %+v", code, got)
		}
	}
}

func TestPaletteIdentityAndRGBRemainDistinct(t *testing.T) {
	for index := range 256 {
		got := tokens(t, fmt.Sprintf("\x1b[38;5;%d;48:5:%d;58:5:%dmX", index, index, index))[0]
		for _, c := range []Color{got.Style.ForegroundColor(), got.Style.BackgroundColor(), got.UnderlineColor} {
			if n, ok := c.Index(); !ok || n != index {
				t.Fatalf("index %d became %v", index, c)
			}
		}
	}
	for index := range 16 {
		fg, bg := 30+index, 40+index
		if index >= 8 {
			fg, bg = 90+index-8, 100+index-8
		}
		got := tokens(t, fmt.Sprintf("\x1b[%d;%dmX", fg, bg))[0]
		if got.Style.ForegroundColor() != indexedColor(index) || got.Style.BackgroundColor() != indexedColor(index) {
			t.Fatalf("basic index %d changed: %+v", index, got)
		}
	}
	got := tokens(t, "\x1b[38;2;0;0;0;48;2;255;255;255;58;2;128;0;0mX\x1b[39;49;59mD")
	for _, c := range []Color{got[0].Style.ForegroundColor(), got[0].Style.BackgroundColor(), got[0].UnderlineColor} {
		if _, ok := c.Index(); ok || c == 0 {
			t.Fatalf("explicit RGB became default/indexed: %v", c)
		}
	}
	if got[1].Style != 0 || got[1].UnderlineColor != 0 {
		t.Fatal("default color resets lost")
	}
}

func TestAttributesSetAndResetIndividually(t *testing.T) {
	for _, tt := range []struct {
		set, reset string
		attr       Style
	}{
		{"1", "22", Bold}, {"2", "22", Dim}, {"3", "23", Italic},
		{"4", "24", Underline}, {"7", "27", Reverse}, {"9", "29", Strike},
	} {
		// The other attributes must survive each reset.
		got := tokens(t, "\x1b[1;2;3;4;7;9mA\x1b["+tt.set+"mB\x1b["+tt.reset+"mC")
		all := Bold | Dim | Italic | Underline | Reverse | Strike
		if got[0].Style != all || got[1].Style != all {
			t.Fatalf("%s: attributes = %b, %b", tt.set, got[0].Style, got[1].Style)
		}
		want := all &^ tt.attr
		if tt.reset == "22" {
			want = all &^ (Bold | Dim)
		}
		if got[2].Style != want {
			t.Fatalf("%s reset by %s = %b, want %b", tt.set, tt.reset, got[2].Style, want)
		}
	}
}

func TestMalformedExtendedColors(t *testing.T) {
	for _, tt := range []struct {
		code string
		want Style
	}{
		// An out-of-range color is skipped with its operands; later values apply.
		{"38;5;300;1", Bold},
		{"38;2;1;2;999;3", Italic},
		// Truncated or unknown extended colors end the sequence.
		{"3;38;5", Italic},
		{"3;48;2;1;2", Italic},
		{"3;38;7;1", Italic},
		// Non-numeric or negative values invalidate the whole sequence.
		{"1;x", 0}, {"-1", 0}, {"1;-3", 0}, {"abc", 0},
		// Unknown attributes are ignored.
		{"5;1", Bold}, {"1;99", Bold},
	} {
		if got := tokens(t, "\x1b["+tt.code+"mX")[0].Style; got != tt.want {
			t.Errorf("%q style = %b, want %b", tt.code, got, tt.want)
		}
	}
	// More than 64 values are refused rather than parsed without bound.
	long := strings.Repeat("1;", 65) + "3"
	if got := tokens(t, "\x1b["+long+"mX")[0].Style; got != 0 {
		t.Errorf("65 values style = %b", got)
	}
}

func TestPaletteApproximations(t *testing.T) {
	for index, want := range map[int]uint32{
		0: 0x000000, 9: 0xff0000, 15: 0xffffff,
		16: 0x000000, 196: 0xff0000, 231: 0xffffff, 67: 0x5f87af,
		232: 0x080808, 244: 0x808080, 255: 0xeeeeee,
	} {
		if got, ok := indexedColor(index).RGB(); !ok || got != want {
			t.Errorf("palette %d = %06x, want %06x", index, got, want)
		}
	}
}
