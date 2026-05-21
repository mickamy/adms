package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mickamy/adms/internal/cli"
	"github.com/mickamy/adms/internal/exit"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintf(stdout, "adms %s\n", version)

			return exit.OK
		case "--help", "-h", "help":
			cli.PrintUsage(stdout)

			return exit.OK
		}
	}

	return cli.Run(args, stdout, stderr)
}
