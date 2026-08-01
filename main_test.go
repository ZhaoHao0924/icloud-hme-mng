package main

import "testing"

func TestValidateAccessConfiguration(t *testing.T) {
	strongToken := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{name: "IPv4 loopback", addr: "127.0.0.1:8081"},
		{name: "IPv6 loopback", addr: "[::1]:8081"},
		{name: "localhost", addr: "localhost:8081"},
		{name: "wildcard without token", addr: ":8081", wantErr: true},
		{name: "IPv4 wildcard without token", addr: "0.0.0.0:8081", wantErr: true},
		{name: "IPv6 wildcard without token", addr: "[::]:8081", wantErr: true},
		{name: "public address without token", addr: "192.0.2.10:8081", wantErr: true},
		{name: "remote with strong token", addr: "0.0.0.0:8081", token: strongToken},
		{name: "short token", addr: "127.0.0.1:8081", token: "too-short", wantErr: true},
		{name: "blank token", addr: "0.0.0.0:8081", token: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAccessConfiguration(tt.addr, tt.token)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAccessConfiguration(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			}
		})
	}
}

func TestIsLoopbackListenAddress(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:8081": true,
		"[::1]:8081":     true,
		"localhost:8081": true,
		"0.0.0.0:8081":   false,
		":8081":          false,
		"invalid":        false,
	}
	for addr, want := range tests {
		if got := isLoopbackListenAddress(addr); got != want {
			t.Errorf("isLoopbackListenAddress(%q) = %v, want %v", addr, got, want)
		}
	}
}
