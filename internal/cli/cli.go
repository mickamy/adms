package cli

import (
	"fmt"
	"io"

	"github.com/mickamy/adms/internal/exit"
)

type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

var commands = []command{
	{"serve", "Run the HTTP API server", notImplemented("serve")},
	{"check", "Verify DB connectivity and schema introspection", check},
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		PrintUsage(stderr)

		return exit.Usage
	}

	name := args[0]
	rest := args[1:]

	for _, c := range commands {
		if c.name == name {
			return c.run(rest, stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "adms: unknown command %q\n", name)
	fmt.Fprintln(stderr, "Run 'adms --help' for a list of commands.")

	return exit.Usage
}

func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, "adms — PostgREST-style HTTP API server for PostgreSQL and MySQL")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "USAGE:")
	fmt.Fprintln(w, "  adms <command> [args...]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "COMMANDS:")

	width := 0

	for _, c := range commands {
		if len(c.name) > width {
			width = len(c.name)
		}
	}

	for _, c := range commands {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.name, c.summary)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "FLAGS:")
	fmt.Fprintln(w, "  --version, -v   Print adms version")
	fmt.Fprintln(w, "  --help, -h      Show this help")
}

func notImplemented(name string) func([]string, io.Writer, io.Writer) int {
	return func(_ []string, _ io.Writer, stderr io.Writer) int {
		fmt.Fprintf(stderr, "adms: %s is not yet implemented\n", name)

		return exit.NotImplemented
	}
}
