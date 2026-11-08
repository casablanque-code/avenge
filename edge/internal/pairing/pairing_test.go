package pairing

import (
	"strings"
	"testing"
)

var sample = Info{Host: "192.168.3.63", Port: 1883, User: "edge", Password: "abc123XYZ"}

func TestRoundTrip(t *testing.T) {
	code, err := sample.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(code)
	if err != nil || got != sample {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestDecodeIgnoresWrapping(t *testing.T) {
	code, _ := sample.Encode()
	wrapped := code[:10] + "\n  " + code[10:20] + " " + code[20:] + "\n"
	if got, err := Decode(wrapped); err != nil || got != sample {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestDecodeCatchesDamage(t *testing.T) {
	code, _ := sample.Encode()
	for name, bad := range map[string]string{
		"truncated": code[:len(code)-12],
		"no prefix": strings.TrimPrefix(code, "avenge1."),
		"flipped":   strings.Replace(code, code[10:11], "Z", 1),
		"empty":     "",
	} {
		if bad == code {
			continue
		}
		if _, err := Decode(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEncodeRejectsBadInfo(t *testing.T) {
	for _, i := range []Info{
		{Host: "", Port: 1883, User: "e", Password: "p"},
		{Host: "h", Port: 0, User: "e", Password: "p"},
		{Host: "h", Port: 1883, User: "", Password: "p"},
		{Host: "a b", Port: 1883, User: "e", Password: "p"},
	} {
		if _, err := i.Encode(); err == nil {
			t.Errorf("%+v accepted", i)
		}
	}
}
