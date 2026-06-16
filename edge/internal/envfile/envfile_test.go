package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}
	return path
}

func TestLoad_BasicKeyValue(t *testing.T) {
	path := writeTempEnv(t, "FOO=bar\nBAZ=qux\n")
	os.Unsetenv("FOO")
	os.Unsetenv("BAZ")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q, want %q", got, "bar")
	}
	if got := os.Getenv("BAZ"); got != "qux" {
		t.Errorf("BAZ = %q, want %q", got, "qux")
	}
}

func TestLoad_SkipsCommentsAndBlankLines(t *testing.T) {
	path := writeTempEnv(t, "# comment\n\nFOO=bar\n  # indented comment\n")
	os.Unsetenv("FOO")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q, want %q", got, "bar")
	}
}

func TestLoad_DoesNotOverrideExistingEnv(t *testing.T) {
	path := writeTempEnv(t, "FOO=from_file\n")
	os.Setenv("FOO", "from_shell")
	defer os.Unsetenv("FOO")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "from_shell" {
		t.Errorf("FOO = %q, want %q (shell value must win)", got, "from_shell")
	}
}

func TestLoad_StripsQuotes(t *testing.T) {
	path := writeTempEnv(t, `FOO="quoted value"`+"\n"+`BAR='single quoted'`+"\n")
	os.Unsetenv("FOO")
	os.Unsetenv("BAR")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "quoted value" {
		t.Errorf("FOO = %q, want %q", got, "quoted value")
	}
	if got := os.Getenv("BAR"); got != "single quoted" {
		t.Errorf("BAR = %q, want %q", got, "single quoted")
	}
}

func TestLoad_MissingFileIsNotAnError(t *testing.T) {
	if err := Load("/nonexistent/path/.env"); err != nil {
		t.Errorf("Load on missing file: got error %v, want nil", err)
	}
}

func TestLoad_IgnoresMalformedLines(t *testing.T) {
	path := writeTempEnv(t, "not-a-kv-line\nFOO=bar\n")
	os.Unsetenv("FOO")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q, want %q", got, "bar")
	}
}
