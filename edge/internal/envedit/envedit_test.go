package envedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetReplacesActiveLine(t *testing.T) {
	f := Parse("# head\nA=1\nB=2\n")
	if err := f.Set("A", "9"); err != nil {
		t.Fatal(err)
	}
	if got := f.String(); got != "# head\nA=9\nB=2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSetUncommentsDocumentedKey(t *testing.T) {
	f := Parse("A=1\n# SENSOR_SAMPLE_RATE=1600\n# note\n")
	f.Set("SENSOR_SAMPLE_RATE", "1600")
	want := "A=1\nSENSOR_SAMPLE_RATE=1600\n# note\n"
	if got := f.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSetIgnoresProseComments(t *testing.T) {
	// "# Real sample rate of the source, in Hz." must not be taken for a key.
	f := Parse("# Real sample rate = the thing\nA=1\n")
	f.Set("Real", "x")
	if !strings.HasSuffix(f.String(), "Real=x\n") {
		t.Fatalf("got %q", f.String())
	}
}

func TestSetAppends(t *testing.T) {
	f := Parse("A=1\n")
	f.Set("B", "2")
	if got := f.String(); got != "A=1\nB=2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSetLastDuplicateWinsLikeLoad(t *testing.T) {
	f := Parse("A=1\nA=2\n")
	f.Set("A", "3")
	if v, _ := f.Get("A"); v != "3" {
		t.Fatalf("Get = %q", v)
	}
	if got := f.String(); got != "A=1\nA=3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSetRefusesBadInput(t *testing.T) {
	f := Parse("")
	if f.Set("A", "x\ny") == nil {
		t.Error("newline in value accepted")
	}
	if f.Set("A B", "x") == nil {
		t.Error("space in key accepted")
	}
}

func TestGetStripsQuotesAndIgnoresComments(t *testing.T) {
	f := Parse("# A=no\nA=\"yes\"\n")
	if v, ok := f.Get("A"); !ok || v != "yes" {
		t.Fatalf("Get = %q %v", v, ok)
	}
	if _, ok := f.Get("B"); ok {
		t.Error("missing key reported found")
	}
}

func TestWriteIs0600AndBackupCopies(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	f := Parse("A=1\n")
	if err := f.Write(p); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	name, err := Backup(p, time.Date(2026, 11, 8, 12, 0, 5, 0, time.UTC))
	if err != nil || filepath.Base(name) != ".env.bak-20261108-120005" {
		t.Fatalf("backup %q %v", name, err)
	}
	if b, _ := os.ReadFile(name); string(b) != "A=1\n" {
		t.Errorf("backup content %q", b)
	}
	if n, err := Backup(filepath.Join(dir, "none"), time.Now()); n != "" || err != nil {
		t.Errorf("missing file: %q %v", n, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".env-*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestUnsetCommentsOutAndSetBringsItBack(t *testing.T) {
	f := Parse("A=1\nSENSOR_SOURCE=mic\nB=2\n")
	if !f.Unset("SENSOR_SOURCE") || f.Unset("SENSOR_SOURCE") {
		t.Fatal("Unset result wrong")
	}
	if got := f.String(); got != "A=1\n# SENSOR_SOURCE=mic\nB=2\n" {
		t.Fatalf("got %q", got)
	}
	if _, ok := f.Get("SENSOR_SOURCE"); ok {
		t.Error("still active")
	}
	f.Set("SENSOR_SOURCE", "mic")
	if got := f.String(); got != "A=1\nSENSOR_SOURCE=mic\nB=2\n" {
		t.Fatalf("got %q", got)
	}
}
