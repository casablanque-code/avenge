package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// box prints a titled frame around lines, wide enough for the longest line.
func box(w io.Writer, title string, lines []string) {
	width := utf8.RuneCountInString(title) + 2
	for _, l := range lines {
		if n := utf8.RuneCountInString(l); n > width {
			width = n
		}
	}
	fmt.Fprintf(w, "┌─ %s %s┐\n", title, strings.Repeat("─", width-utf8.RuneCountInString(title)-1))
	for _, l := range lines {
		fmt.Fprintf(w, "│ %s%s │\n", l, strings.Repeat(" ", width-utf8.RuneCountInString(l)))
	}
	fmt.Fprintf(w, "└%s┘\n", strings.Repeat("─", width+2))
}

// printFlags lists a command's flags in the same "--name" form as the docs.
func printFlags(w io.Writer, fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		head := "  --" + f.Name
		if name != "" && name != "value" {
			head += " " + strings.ToUpper(name)
		} else if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !isBool {
			head += " VALUE"
		}
		if f.DefValue != "" && f.DefValue != "0" && f.DefValue != "false" {
			usage += " (default " + f.DefValue + ")"
		}
		fmt.Fprintf(w, "%-24s %s\n", head, usage)
	})
}
