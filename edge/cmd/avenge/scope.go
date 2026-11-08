package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
)

// coreOnlyKeys exist in core/.env and never in an edge .env.
var coreOnlyKeys = []string{
	"GRAFANA_ADMIN_PASSWORD",
	"CLICKHOUSE_PASSWORD",
	"MQTT_EDGE_PASSWORD",
	"MQTT_TELEGRAF_PASSWORD",
}

// isCoreDir reports whether dir is the core stack's own directory.
func isCoreDir(dir string) bool {
	for _, n := range []string{"compose.yaml", "Caddyfile"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			return false
		}
	}
	return true
}

// looksLikeCoreEnv reports whether f holds core secrets.
func looksLikeCoreEnv(f *envedit.File) bool {
	for _, k := range coreOnlyKeys {
		if _, ok := f.Get(k); ok {
			return true
		}
	}
	return false
}

// edgeScopeError refuses to treat a core directory or a core .env as an edge
// one. Without it, `avenge edge setup` run inside core/ would write the
// sensor settings into the core's .env.
func edgeScopeError(dir, envPath string, f *envedit.File) error {
	if isCoreDir(dir) {
		return fmt.Errorf("%s is the core directory (compose.yaml and Caddyfile of the core stack).\n"+
			"The edge setup writes the .env that edge-filter reads, so run it where edge-filter runs:\n"+
			"in the folder with the edge-filter binary, or in the repository root (it then uses edge/.env).\n"+
			"The core is set up with: avenge core setup", dir)
	}
	if f != nil && looksLikeCoreEnv(f) {
		return fmt.Errorf("%s is a core .env (it holds GRAFANA_ADMIN_PASSWORD and the other core passwords); "+
			"the edge setup would mix sensor settings into it.\n"+
			"Run avenge edge setup in the folder of edge-filter, which has its own .env", envPath)
	}
	return nil
}
