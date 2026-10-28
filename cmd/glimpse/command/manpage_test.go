package command

import (
	"os"
	"strings"
	"testing"

	"glimpse/internal/keybind"

	"github.com/spf13/pflag"
)

// The man page is written by hand; this keeps it from drifting from the code.
func TestManPageDocumentsEveryOptionAndAction(t *testing.T) {
	data, err := os.ReadFile("../../../docs/glimpse.1")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(data), `\-`, "-")
	root := NewRoot()
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	check := func(flag *pflag.Flag) {
		if !flag.Hidden && !strings.Contains(page, "--"+flag.Name) {
			t.Errorf("man page lacks --%s", flag.Name)
		}
	}
	root.Flags().VisitAll(check)
	root.PersistentFlags().VisitAll(check)
	view, prompt := keybind.Actions()
	for _, action := range append(view, prompt...) {
		if !strings.Contains(page, string(action)) {
			t.Errorf("man page lacks keybind action %s", action)
		}
	}
}
