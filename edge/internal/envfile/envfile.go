// Package envfile loads KEY=VALUE pairs from a .env file into the process
// environment. It exists so the edge binary behaves identically whether it
// runs directly on a dev machine or inside the Docker container — both read
// the same edge/.env, instead of .env being Docker-only.
//
// Design choices, deliberately minimal:
//   - No external dependencies (matches the rest of this module).
//   - Already-set environment variables always win. This means
//     `MQTT_BROKER_URL=foo ./edge-filter` overrides .env without editing it,
//     and explicit CLI flags (handled in main.go) still take priority over both.
//   - Missing file is not an error — local dev without a .env, or a
//     container that gets all its config via `environment:` in compose,
//     both work unchanged.
//   - No interpolation, no multiline values, no export keyword. If the
//     project ever needs those, reach for a real library instead of
//     growing this file into one.
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// Load reads path as a .env file and calls os.Setenv for each KEY=VALUE
// line, skipping any key that is already set in the environment.
// It silently does nothing if path does not exist.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue // not a KEY=VALUE line — ignore rather than fail
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Strip a single layer of surrounding quotes, if present.
		value = strings.Trim(value, `"'`)

		if key == "" {
			continue
		}

		// Never override a value the caller already set — shell exports
		// and `docker run -e` must always win over the file.
		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}

		os.Setenv(key, value)
	}

	return scanner.Err()
}
