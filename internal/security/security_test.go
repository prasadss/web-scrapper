package security

import (
	"context"
	"fmt"
	"net"
	"testing"
)

type fakeResolver struct {
	hosts map[string][]net.IP
}

func (r fakeResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	ips, ok := r.hosts[host]
	if !ok {
		return nil, fmt.Errorf("host not found: %s", host)
	}
	return ips, nil
}

func TestValidateURL_BlockedPatterns(t *testing.T) {
	validator := NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"example.com": {net.ParseIP("93.184.216.34")},
		},
	})

	tests := []struct {
		name    string
		url     string
		blocked bool
	}{
		{"phpmyadmin", "https://example.com/phpmyadmin", true},
		{"wp-admin", "https://example.com/wp-admin/login.php", true},
		{"adminer", "https://example.com/adminer", true},
		{"debug pprof", "https://example.com/debug/pprof", true},
		{"mongodb port", "https://example.com:27017", true},
		{"redis port", "https://example.com:6379", true},
		{"mysql port", "https://example.com:3306", true},
		{"normal URL", "https://example.com/about", false},
		{"normal port", "https://example.com:443/page", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateURL(context.Background(), tt.url)
			if tt.blocked && err == nil {
				t.Errorf("expected URL %q to be blocked", tt.url)
			}
			if !tt.blocked && err != nil {
				t.Errorf("expected URL %q to be allowed, got error: %v", tt.url, err)
			}
		})
	}
}

func TestValidateURL_PrivateIPs(t *testing.T) {
	validator := NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"localhost": {net.ParseIP("127.0.0.1")},
		},
	})

	tests := []struct {
		name    string
		url     string
		blocked bool
	}{
		{"localhost", "https://localhost/admin", true},
		{"loopback IP", "https://127.0.0.1/secret", true},
		{"private 10.x", "https://10.0.0.1/internal", true},
		{"private 192.168.x", "https://192.168.1.1/router", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateURL(context.Background(), tt.url)
			if tt.blocked && err == nil {
				t.Errorf("expected URL %q to be blocked (private IP)", tt.url)
			}
		})
	}
}
