package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mreview <paper.tex>")
		os.Exit(2)
	}

	fmt.Fprintf(os.Stderr, "mreview: not implemented yet (arg: %s)\n", flag.Arg(0))
	os.Exit(1)
}
