// Package netinfo finds the addresses a core machine could listen on and
// recognises WSL, so the setup wizard can offer sensible answers.
package netinfo

import (
	"net"
	"os"
	"strings"
)

// Addr is one candidate LAN address.
type Addr struct {
	Iface string
	IP    string
}

// Iface is the part of net.Interface the filter needs (so tests need no network).
type Iface struct {
	Name  string
	Flags net.Flags
	Addrs []*net.IPNet
}

// virtualPrefixes are interface names that are containers, VPNs or tunnels,
// not the LAN an edge device is on.
var virtualPrefixes = []string{
	"lo", "docker", "br-", "veth", "virbr", "tailscale", "amn", "tun", "tap",
	"wg", "zt", "utun", "vmnet", "vboxnet", "cni", "flannel",
}

// LAN picks IPv4 addresses that look like a real LAN: interface up, not a
// loopback, container bridge or VPN, no /32 host route, no link-local.
func LAN(ifaces []Iface) []Addr {
	var out []Addr
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		skip := false
		for _, p := range virtualPrefixes {
			if strings.HasPrefix(ifc.Name, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, a := range ifc.Addrs {
			ip := a.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			if ones, _ := a.Mask.Size(); ones >= 32 {
				continue
			}
			out = append(out, Addr{Iface: ifc.Name, IP: ip.String()})
		}
	}
	return out
}

// LocalLAN lists the candidates of this machine.
func LocalLAN() []Addr {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var list []Iface
	for _, ifc := range ifs {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		item := Iface{Name: ifc.Name, Flags: ifc.Flags}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				item.Addrs = append(item.Addrs, n)
			}
		}
		list = append(list, item)
	}
	return LAN(list)
}

// IsWSLVersion tells from the text of /proc/version whether the kernel is WSL's.
func IsWSLVersion(procVersion string) bool {
	v := strings.ToLower(procVersion)
	return strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
}

// IsWSL reports whether this process runs inside WSL.
func IsWSL() bool {
	b, err := os.ReadFile("/proc/version")
	return err == nil && IsWSLVersion(string(b))
}
