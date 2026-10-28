package keybind

import "testing"

func TestEveryActionIsDescribed(t *testing.T) {
	view, prompt := Actions()
	if view[0] != Quit || prompt[0] != Accept {
		t.Fatalf("actions are not in default order: %v %v", view[0], prompt[0])
	}
	for _, action := range append(view, prompt...) {
		if Descriptions[action] == "" {
			t.Errorf("%s has no description", action)
		}
	}
	if got := Format([]Key{"g", "?"}); got != "g?" {
		t.Fatalf("Format = %q", got)
	}
	if got := Format([]Key{" ", "<C-c>"}); got != "<Space><C-c>" {
		t.Fatalf("Format = %q", got)
	}
}
