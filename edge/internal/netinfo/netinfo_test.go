package netinfo

import (
	"net"
	"testing"
)

func ipnet(s string) *net.IPNet {
	ip, n, _ := net.ParseCIDR(s)
	n.IP = ip
	return n
}

func TestLANKeepsRealInterfacesOnly(t *testing.T) {
	up := net.FlagUp | net.FlagBroadcast
	got := LAN([]Iface{
		{Name: "lo", Flags: up | net.FlagLoopback, Addrs: []*net.IPNet{ipnet("127.0.0.1/8")}},
		{Name: "wlp1s0", Flags: up, Addrs: []*net.IPNet{ipnet("192.168.3.16/24"), ipnet("fe80::1/64")}},
		{Name: "docker0", Flags: up, Addrs: []*net.IPNet{ipnet("172.17.0.1/16")}},
		{Name: "tailscale0", Flags: up | net.FlagPointToPoint, Addrs: []*net.IPNet{ipnet("100.84.205.85/32")}},
		{Name: "amn0", Flags: up, Addrs: []*net.IPNet{ipnet("10.8.1.13/32")}},
		{Name: "eth1", Flags: 0, Addrs: []*net.IPNet{ipnet("10.0.0.5/24")}},
		{Name: "eth2", Flags: up, Addrs: []*net.IPNet{ipnet("169.254.1.1/16")}},
	})
	if len(got) != 1 || got[0].IP != "192.168.3.16" || got[0].Iface != "wlp1s0" {
		t.Fatalf("got %+v", got)
	}
}

func TestIsWSLVersion(t *testing.T) {
	if !IsWSLVersion("Linux version 5.15.153.1-microsoft-standard-WSL2 (root@x)") {
		t.Error("WSL2 not recognised")
	}
	if IsWSLVersion("Linux version 6.8.0-45-generic (buildd@lcy02)") {
		t.Error("plain Ubuntu taken for WSL")
	}
}
