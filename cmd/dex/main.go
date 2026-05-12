package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/scshafe/dex/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		os.Exit(runLs(os.Args[2:]))
	case "version":
		fmt.Println("dex 0.0.0-dev")
	default:
		fmt.Fprintf(os.Stderr, "dex: unknown verb %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runLs(args []string) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON instead of human-readable output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return cli.RunLs(cli.LsOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
		JSON:      *jsonOut,
	}, fs.Args())
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: dex <verb> [args]

Verbs:
  ls [--json] [<uuid>|<path>]
                         List entries. With no arg, the merged root.
                         <uuid> looks up a rolodex directly.
                         <path> starts with "/" (e.g. "/tools" or
                         "/tools/hammer") and walks pointers.
  version                Print version

Environment:
  DEX_STORE              Path to the store root (must contain
                         bundled/personal/private/ephemeral dirs)`)
}
