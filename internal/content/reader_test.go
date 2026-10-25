package content

import (
	"errors"
	"glimpse/internal/pager"
	"io"
	"strings"
	"testing"
	"unicode"
)

func tokens(t *testing.T, input string) []Token {
	t.Helper()
	reader := NewReader(strings.NewReader(input), 0, int64(len(input)), 0, true)
	var result []Token
	for {
		token, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !token.Hidden {
			result = append(result, token)
		}
	}
	return result
}

func TestSupportedStylesAndSuppressedControls(t *testing.T) {
	input := "\x1b[1;3;4;7;9;38;2;1;2;3;48;5;196mA\x1b[0mB\x1b[2J\x1b]52;c;secret\a\x1b]8;;https://example.invalid\x1b\\C\x1b]8;;\x1b\\\x1b(BD"
	got := tokens(t, input)
	if len(got) != 4 || got[0].Rune != 'A' || got[1].Rune != 'B' || got[2].Rune != 'C' || got[3].Rune != 'D' {
		t.Fatalf("visible tokens = %+v", got)
	}
	style := got[0].Style
	if style&(Bold|Italic|Underline|Reverse|Strike) != Bold|Italic|Underline|Reverse|Strike {
		t.Fatalf("attributes = %x", style)
	}
	if fg, ok := style.Foreground(); !ok || fg != 0x010203 {
		t.Fatalf("foreground = %x, %v", fg, ok)
	}
	if bg, ok := style.Background(); !ok || bg != 0xff0000 {
		t.Fatalf("background = %x, %v", bg, ok)
	}
	if got[1].Style != 0 {
		t.Fatal("SGR reset did not restore defaults")
	}
	if got := tokens(t, "\u009dsecret\u009cvisible"); len(got) != 7 || got[0].Rune != 'v' {
		t.Fatalf("C1 string = %+v", got)
	}
}

func TestEveryStreamingBoundary(t *testing.T) {
	input := "hé\x1b[32mllo\x1b]0;hidden\a界"
	for split := 0; split <= len(input); split++ {
		doc := pager.NewDocument()
		position := int64(0)
		style := Style(0)
		var output strings.Builder
		for part, data := range []string{input[:split], input[split:]} {
			doc.Write([]byte(data))
			reader := NewReader(doc, position, doc.Len(), style, part == 1)
			for {
				token, err := reader.Next()
				if errors.Is(err, io.EOF) || errors.Is(err, ErrIncomplete) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				position, style = token.End, token.Style
				if !token.Hidden {
					output.WriteRune(token.Rune)
				}
			}
		}
		doc.Close()
		if output.String() != "héllo界" {
			t.Fatalf("split %d: %q", split, output.String())
		}
		if _, ok := style.Foreground(); !ok {
			t.Fatalf("split %d lost style", split)
		}
	}
}

func TestMalformedAndOversizedInputIsSafe(t *testing.T) {
	for _, input := range []string{"\x1b", "\x1b[31", "\x1b]unterminated", "\x1b[" + strings.Repeat(";", 8192) + "mX", "\x1b]" + strings.Repeat("x", 8192) + "\aX", "\x00\x07\x80"} {
		for _, token := range tokens(t, input) {
			if unicode.IsControl(token.Rune) && !strings.ContainsRune("\r\n\t\b", token.Rune) {
				t.Fatalf("unsafe rune %U", token.Rune)
			}
		}
	}
}

func FuzzSafeContent(f *testing.F) {
	for _, input := range []string{"hello", "\x1b[31mred", "\x1b]52;secret\a", "界\b_"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16384 {
			t.Skip()
		}
		for _, token := range tokens(t, input) {
			if unicode.IsControl(token.Rune) && !strings.ContainsRune("\r\n\t\b", token.Rune) {
				t.Fatalf("unsafe %U", token.Rune)
			}
		}
	})
}
