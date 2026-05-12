package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		// wired up in Task 10
		fmt.Println("ls: not yet implemented")
	case "version":
		fmt.Println("dex 0.0.0-dev")
	default:
		fmt.Fprintf(os.Stderr, "dex: unknown verb %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: dex <verb> [args]

Verbs:
  ls [<uuid>]   List entries (merged root, or a specific rolodex)
  version       Print version`)
}
