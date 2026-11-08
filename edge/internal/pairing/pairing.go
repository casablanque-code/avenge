// Package pairing turns the four things an edge device needs from the core
// (host, port, MQTT user, MQTT password) into one line that can be pasted.
//
// The code is NOT encrypted: it is base64 plus a checksum that catches a
// truncated or mistyped paste. Treat it exactly like the password inside it.
package pairing

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

const prefix = "avenge1."

// Info is what a pairing code carries.
type Info struct {
	Host     string `json:"h"`
	Port     int    `json:"p"`
	User     string `json:"u"`
	Password string `json:"w"`
}

func (i Info) validate() error {
	if i.Host == "" || strings.ContainsAny(i.Host, " \t\r\n/") {
		return fmt.Errorf("bad host %q", i.Host)
	}
	if i.Port < 1 || i.Port > 65535 {
		return fmt.Errorf("bad port %d", i.Port)
	}
	if i.User == "" || i.Password == "" {
		return errors.New("user or password is empty")
	}
	return nil
}

// Encode returns "avenge1.<base64url>.<crc32 hex>".
func (i Info) Encode() (string, error) {
	if err := i.validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return fmt.Sprintf("%s%s.%08x", prefix, body, crc32.ChecksumIEEE([]byte(body))), nil
}

// Decode parses a code. Spaces and line breaks (from a wrapped paste) are ignored.
func Decode(code string) (Info, error) {
	code = strings.Join(strings.Fields(code), "")
	if !strings.HasPrefix(code, prefix) {
		return Info{}, errors.New("not a pairing code (it starts with \"avenge1.\")")
	}
	rest := strings.TrimPrefix(code, prefix)
	body, sum, ok := strings.Cut(rest, ".")
	if !ok || body == "" {
		return Info{}, errors.New("pairing code is incomplete: copy the whole line")
	}
	if want := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(body))); sum != want {
		return Info{}, errors.New("checksum mismatch: the code was cut or mistyped, copy it again")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Info{}, errors.New("pairing code is damaged")
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return Info{}, errors.New("pairing code is damaged")
	}
	if err := info.validate(); err != nil {
		return Info{}, fmt.Errorf("pairing code is invalid: %w", err)
	}
	return info, nil
}
