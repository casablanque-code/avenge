// Package probe answers "can this machine talk to the broker, and if not,
// where does it stop?". The edge setup and the doctor both use it, so they
// give the same diagnosis.
package probe

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/mqtt"
)

// Kind says where the connection stopped.
type Kind string

const (
	OK       Kind = "ok"
	DNS      Kind = "dns"      // the name does not resolve
	Timeout  Kind = "timeout"  // nothing answered: routing, firewall, wrong address, VPN
	Refused  Kind = "refused"  // the machine answered, nothing listens on the port
	NoReply  Kind = "noreply"  // TCP connects but the broker does not speak MQTT
	Login    Kind = "login"    // the broker refused the user or password
	Network  Kind = "network"  // some other network error (unreachable, ...)
	Internal Kind = "internal" // anything else
)

// Result is the outcome of one check.
type Result struct {
	Kind Kind
	Err  error
}

// OK reports whether the login worked.
func (r Result) OK() bool { return r.Kind == OK }

// Broker dials host:port and logs in with the given MQTT user.
func Broker(host string, port int, user, password string, timeout time.Duration) Result {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return Result{Kind: classifyDial(err), Err: err}
	}
	conn.Close()

	cfg := mqtt.DefaultConfig(addr, fmt.Sprintf("avenge-probe-%d", os.Getpid()))
	cfg.ConnectTimeout = timeout
	cfg.KeepAlive = 0
	cfg.Username, cfg.Password = user, password
	c, err := mqtt.Connect(cfg)
	if err != nil {
		return Result{Kind: classifyConnect(err), Err: err}
	}
	c.Disconnect()
	return Result{Kind: OK}
}

func classifyDial(err error) Kind {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns):
		return DNS
	case errors.Is(err, syscall.ECONNREFUSED):
		return Refused
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return Timeout
	}
	return Network
}

func classifyConnect(err error) Kind {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "broker refused connection"):
		return Login
	case strings.Contains(msg, "dial"):
		return classifyDial(errors.Unwrap(err))
	}
	return NoReply
}

// Hint says what to check next for a failed result. It names the commands to
// run, so the answer is one paste away.
func Hint(r Result, host string) string {
	switch r.Kind {
	case DNS:
		return fmt.Sprintf("%q does not resolve. Use the IP address of the core.", host)
	case Timeout:
		return "Nothing answers (the packets get lost on the way). Check, in this order:\n" +
			"  here:  ping -c2 " + host + "   and   ip route get " + host + "\n" +
			"         the route must name the LAN interface, not a VPN (tailscale, amn, wg, tun).\n" +
			"  core:  MQTT_BIND in core/.env must be the LAN address (or 0.0.0.0), then\n" +
			"         ss -ltn 'sport = :1883' shows it; the firewall must allow the port.\n" +
			"  core in WSL2: see \"Core inside WSL2\" in docs/OPERATIONS.md.\n" +
			"  Wi-Fi: some routers block traffic between clients (client isolation)."
	case Refused:
		return "The machine answers but nothing listens on that port: the core is not running, or MQTT_BIND is another address.\n" +
			"  core:  docker compose -f core/compose.yaml ps   and   ss -ltn 'sport = :1883'"
	case NoReply:
		return "The port is open but the broker does not answer MQTT. Something else may listen there, or the core is still starting.\n" +
			"  core:  docker logs sm_mosquitto"
	case Login:
		return "The broker is reachable but refused the login: MQTT_PASSWORD here must equal MQTT_EDGE_PASSWORD in core/.env.\n" +
			"  Print the code again on the core: avenge core pair, then run avenge edge setup here."
	case Network:
		return "The network refused the connection (unreachable host or network). Check the address and the route: ip route get " + host
	}
	return ""
}
