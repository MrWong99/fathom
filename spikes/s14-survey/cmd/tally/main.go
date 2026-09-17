// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

// Command tally prints the S14 result tables from a CSV export of the answers.
package main

import (
	"fmt"
	"os"

	"github.com/MrWong99/fathom/spikes/s14-survey/tally"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tally responses.csv")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	rs, err := tally.ReadCSV(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(tally.Markdown(rs))
}
