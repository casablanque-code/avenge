// Package envedit changes values in a .env file without touching anything
// else: comments, blank lines and the order of the lines stay as they are.
// It is the writing counterpart of package envfile, which only reads.
//
// Rules, kept deliberately small:
//   - Set replaces the active "KEY=value" line if there is one.
//   - Otherwise it turns the first commented "# KEY=value" line into an active
//     one (that is how the .env.example files document optional settings).
//   - Otherwise it appends "KEY=value" at the end.
//   - Values are written bare, without quotes; a value with a newline is refused.
package envedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File is the text of a .env file, line by line.
type File struct {
	lines []string
}

// Parse splits text into lines. A trailing newline does not create an empty line.
func Parse(text string) *File {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return &File{}
	}
	return &File{lines: strings.Split(text, "\n")}
}

// Read loads path. A missing file gives an empty File and no error.
func Read(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, err
	}
	return Parse(string(b)), nil
}

// String returns the file text with a final newline (empty for an empty file).
func (f *File) String() string {
	if len(f.lines) == 0 {
		return ""
	}
	return strings.Join(f.lines, "\n") + "\n"
}

// keyOf returns the key of an active "KEY=value" line.
func keyOf(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", false
	}
	k, _, ok := strings.Cut(t, "=")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(k), true
}

// commentedKeyOf returns the key of a commented-out "# KEY=value" line.
func commentedKeyOf(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return "", false
	}
	t = strings.TrimSpace(strings.TrimLeft(t, "#"))
	k, _, ok := strings.Cut(t, "=")
	if !ok {
		return "", false
	}
	k = strings.TrimSpace(k)
	if k == "" || strings.ContainsAny(k, " \t") {
		return "", false
	}
	for _, r := range k {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return "", false
		}
	}
	return k, true
}

// Get returns the value of an active line (last one wins, like a shell would
// source it) with one layer of quotes removed.
func (f *File) Get(key string) (string, bool) {
	val, found := "", false
	for _, line := range f.lines {
		if k, ok := keyOf(line); ok && k == key {
			_, v, _ := strings.Cut(strings.TrimSpace(line), "=")
			val, found = strings.Trim(strings.TrimSpace(v), `"'`), true
		}
	}
	return val, found
}

// Set writes KEY=value following the rules in the package comment.
func (f *File) Set(key, value string) error {
	if key == "" || strings.ContainsAny(key, "= \t\n") {
		return fmt.Errorf("envedit: bad key %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("envedit: value of %s contains a line break", key)
	}
	line := key + "=" + value
	last := -1
	for i, l := range f.lines {
		if k, ok := keyOf(l); ok && k == key {
			last = i
		}
	}
	if last >= 0 {
		f.lines[last] = line
		return nil
	}
	for i, l := range f.lines {
		if k, ok := commentedKeyOf(l); ok && k == key {
			f.lines[i] = line
			return nil
		}
	}
	f.lines = append(f.lines, line)
	return nil
}

// Unset comments out every active KEY line ("# KEY=value"), so the setting is
// off but still documented and a later Set turns it on again in place.
// It reports whether anything was active.
func (f *File) Unset(key string) bool {
	changed := false
	for i, l := range f.lines {
		if k, ok := keyOf(l); ok && k == key {
			f.lines[i] = "# " + strings.TrimSpace(l)
			changed = true
		}
	}
	return changed
}

// Write stores the file at path with mode 0600, through a temporary file so a
// crash never leaves half a .env behind.
func (f *File) Write(path string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".env-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(f.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Backup copies an existing file next to itself as <path>.bak-YYYYMMDD-HHMMSS
// (mode 0600) and returns the new name. A missing file is not an error: the
// returned name is empty.
func Backup(path string, now time.Time) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	name := path + ".bak-" + now.Format("20060102-150405")
	if err := os.WriteFile(name, b, 0o600); err != nil {
		return "", err
	}
	return name, nil
}
