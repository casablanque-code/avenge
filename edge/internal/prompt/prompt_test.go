package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newP(in string) (*Prompter, *bytes.Buffer) {
	var out bytes.Buffer
	return New(strings.NewReader(in), &out), &out
}

func TestAskDefaultAndValidation(t *testing.T) {
	p, out := newP("bad\n\n")
	v, err := p.Ask("Name", "dflt", func(s string) error {
		if s == "bad" {
			return errors.New("not allowed")
		}
		return nil
	})
	if err != nil || v != "dflt" {
		t.Fatalf("got %q %v", v, err)
	}
	if !strings.Contains(out.String(), "not allowed") {
		t.Errorf("reason not shown: %q", out.String())
	}
}

func TestAskEOF(t *testing.T) {
	p, _ := newP("")
	if _, err := p.Ask("x", "", nil); !errors.Is(err, ErrNoInput) {
		t.Fatalf("err = %v", err)
	}
}

func TestAskLastLineWithoutNewline(t *testing.T) {
	p, _ := newP("abc")
	if v, err := p.Ask("x", "", nil); err != nil || v != "abc" {
		t.Fatalf("got %q %v", v, err)
	}
}

func TestConfirm(t *testing.T) {
	p, _ := newP("maybe\nY\n\n")
	if v, err := p.Confirm("q", false); err != nil || !v {
		t.Fatalf("first: %v %v", v, err)
	}
	if v, err := p.Confirm("q", false); err != nil || v {
		t.Fatalf("default: %v %v", v, err)
	}
}

func TestChoose(t *testing.T) {
	p, _ := newP("9\n2\n\n")
	if i, err := p.Choose("pick", []string{"a", "b", "c"}, 0); err != nil || i != 1 {
		t.Fatalf("got %d %v", i, err)
	}
	if i, _ := p.Choose("pick", []string{"a", "b", "c"}, 2); i != 2 {
		t.Fatalf("default %d", i)
	}
}
