package main

import "testing"

func TestDefaultBroker(t *testing.T) {
	cases := []struct {
		name, host, port, want string
	}{
		{"defaults", "", "", "localhost:1883"},
		{"ipv4", "192.168.1.50", "1883", "192.168.1.50:1883"},
		{"hostname", "broker.local", "8883", "broker.local:8883"},
		{"ipv6 bare", "fe80::1", "1883", "[fe80::1]:1883"},
		{"ipv6 bracketed", "[::1]", "1883", "[::1]:1883"},
		{"default port only", "10.0.0.7", "", "10.0.0.7:1883"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CENTRAL_HOST", c.host)
			t.Setenv("MQTT_PORT", c.port)
			if got := defaultBroker(); got != c.want {
				t.Fatalf("defaultBroker() = %q, want %q", got, c.want)
			}
		})
	}
}
