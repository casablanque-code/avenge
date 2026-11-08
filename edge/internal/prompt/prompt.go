// Package prompt asks questions on a terminal. It is plain line input (no raw
// mode, no dependencies), so it behaves the same over ssh, in WSL and in tests.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrNoInput is returned when the input ends before an answer was given.
var ErrNoInput = errors.New("no input: the answer stream ended")

// Prompter reads answers from In and writes questions to Out.
type Prompter struct {
	In  *bufio.Reader
	Out io.Writer
}

// New builds a Prompter.
func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: bufio.NewReader(in), Out: out}
}

// IsTerminal reports whether f is an interactive terminal (a character device).
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func (p *Prompter) line() (string, error) {
	s, err := p.In.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", ErrNoInput
	}
	return strings.TrimSpace(s), nil
}

// Ask asks for a text value. An empty answer takes def. A failed validation
// prints the reason and asks again.
func (p *Prompter) Ask(label, def string, validate func(string) error) (string, error) {
	for {
		if def != "" {
			fmt.Fprintf(p.Out, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(p.Out, "%s: ", label)
		}
		ans, err := p.line()
		if err != nil {
			return "", err
		}
		if ans == "" {
			ans = def
		}
		if validate != nil {
			if verr := validate(ans); verr != nil {
				fmt.Fprintf(p.Out, "  ✗ %v\n", verr)
				continue
			}
		}
		return ans, nil
	}
}

// Confirm asks a yes/no question.
func (p *Prompter) Confirm(label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(p.Out, "%s [%s]: ", label, hint)
		ans, err := p.line()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(ans) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.Out, "  ✗ answer y or n")
	}
}

// Choose shows a numbered list and returns the index picked (def is the index
// taken on an empty answer).
func (p *Prompter) Choose(label string, options []string, def int) (int, error) {
	fmt.Fprintln(p.Out, label)
	for i, o := range options {
		mark := " "
		if i == def {
			mark = "*"
		}
		fmt.Fprintf(p.Out, "  %s %d) %s\n", mark, i+1, o)
	}
	for {
		fmt.Fprintf(p.Out, "Number [%d]: ", def+1)
		ans, err := p.line()
		if err != nil {
			return 0, err
		}
		if ans == "" {
			return def, nil
		}
		n, convErr := strconv.Atoi(ans)
		if convErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintf(p.Out, "  ✗ enter a number from 1 to %d\n", len(options))
	}
}
