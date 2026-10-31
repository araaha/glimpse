package command

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// nushellCompleter completes flag values by asking glimpse itself through
// Cobra's hidden __complete command, as the other shells' scripts do. A
// context ending in a space splits into a final empty word, which asks for
// all candidates. Cobra ends its output with a :directive line and reports
// it again on standard error, which complete keeps off the screen.
const nushellCompleter = `def "nu-complete glimpse" [context: string] {
    let words = ($context | str trim --left | split row --regex '\s+' | skip 1)
    (^glimpse __complete ...$words | complete).stdout
    | lines
    | where {|line| not ($line | str starts-with ':') }
    | each {|line|
        let parts = ($line | split row "\t")
        let description = if ($parts | length) > 1 { $parts.1 } else { '' }
        {value: $parts.0, description: $description}
    }
}

`

// writeNushellCompletion prints a Nushell extern definition for root. Cobra
// has no Nushell generator.
//
// Boolean flags are left out: Nushell passes a declared --wrap=false as the
// two arguments --wrap and false, which Cobra reads as --wrap and a file
// named false. Undeclared flags reach glimpse unchanged.
func writeNushellCompletion(root *cobra.Command, out io.Writer) error {
	var b strings.Builder
	b.WriteString(nushellCompleter)
	fmt.Fprintf(&b, "export extern %q [\n", root.Name())
	root.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden || flag.Value.Type() == "bool" {
			return
		}
		fmt.Fprintf(&b, "    --%s", flag.Name)
		if flag.Shorthand != "" {
			fmt.Fprintf(&b, "(-%s)", flag.Shorthand)
		}
		fmt.Fprintf(&b, ": %s # %s\n", nushellType(root, flag), flag.Usage)
	})
	b.WriteString("    file?: path # file to show; standard input when omitted or -\n]\n")
	_, err := io.WriteString(out, b.String())
	return err
}

func nushellType(root *cobra.Command, flag *pflag.Flag) string {
	if _, ok := flag.Annotations[cobra.BashCompSubdirsInDir]; ok {
		return "directory"
	}
	if _, ok := flag.Annotations[cobra.BashCompFilenameExt]; ok {
		return "path"
	}
	kind := "string"
	switch flag.Value.Type() {
	case "int", "int64":
		kind = "int"
	}
	if _, ok := root.GetFlagCompletionFunc(flag.Name); ok {
		kind += `@"nu-complete glimpse"`
	}
	return kind
}
