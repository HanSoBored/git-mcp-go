package client

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_FromRetryAfterHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "30")
	got := parseRetryAfter(resp)
	expected := 30 * time.Second
	if got != expected {
		t.Errorf("parseRetryAfter = %v, want %v", got, expected)
	}
}

func TestParseRetryAfter_FromRateLimitReset(t *testing.T) {
	future := time.Now().Add(45 * time.Second).Unix()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", future))
	got := parseRetryAfter(resp)
	if got < 43*time.Second || got > 47*time.Second {
		t.Errorf("parseRetryAfter = %v, want ~45s", got)
	}
}

func TestParseRetryAfter_Default(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	got := parseRetryAfter(resp)
	expected := 60 * time.Second
	if got != expected {
		t.Errorf("parseRetryAfter = %v, want %v", got, expected)
	}
}

func TestParseRetryAfter_RetryAfterPrecedence(t *testing.T) {
	future := time.Now().Add(90 * time.Second).Unix()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "15")
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", future))
	got := parseRetryAfter(resp)
	expected := 15 * time.Second
	if got != expected {
		t.Errorf("parseRetryAfter = %v, want %v (Retry-After should take precedence)", got, expected)
	}
}

func TestParseRetryAfter_RetryAfterNegative(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "-1")
	got := parseRetryAfter(resp)
	expected := 60 * time.Second
	if got != expected {
		t.Errorf("parseRetryAfter = %v, want %v", got, expected)
	}
}

func TestParseRetryAfter_ZeroValue(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "0")
	got := parseRetryAfter(resp)
	expected := minRetryBackoff
	if got != expected {
		t.Errorf("parseRetryAfter(0) = %v, want %v (zero should clamp to minRetryBackoff)", got, expected)
	}
}

func TestParseRetryAfter_ResetInPast(t *testing.T) {
	past := time.Now().Add(-10 * time.Second).Unix()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", past))
	got := parseRetryAfter(resp)
	expected := 60 * time.Second
	if got != expected {
		t.Errorf("parseRetryAfter = %v, want %v", got, expected)
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		// RFC 1918 private ranges
		{name: "RFC1918 10.0.0.1", ip: "10.0.0.1", want: true},
		{name: "RFC1918 172.16.0.1", ip: "172.16.0.1", want: true},
		{name: "RFC1918 172.31.255.255", ip: "172.31.255.255", want: true},
		{name: "RFC1918 192.168.0.1", ip: "192.168.0.1", want: true},
		{name: "RFC1918 192.168.255.255", ip: "192.168.255.255", want: true},

		// Loopback
		{name: "Loopback 127.0.0.1", ip: "127.0.0.1", want: true},
		{name: "Loopback 127.255.255.255", ip: "127.255.255.255", want: true},

		// Link-local
		{name: "LinkLocal 169.254.1.1", ip: "169.254.1.1", want: true},
		{name: "LinkLocal 169.254.169.254", ip: "169.254.169.254", want: true},

		// CGNAT (RFC 6598) 100.64.0.0/10
		{name: "CGNAT 100.64.0.1", ip: "100.64.0.1", want: true},
		{name: "CGNAT 100.127.255.255", ip: "100.127.255.255", want: true},

		// Public addresses
		{name: "Public 8.8.8.8", ip: "8.8.8.8", want: false},
		{name: "Public 1.1.1.1", ip: "1.1.1.1", want: false},
		{name: "Public 140.82.121.4 (github.com)", ip: "140.82.121.4", want: false},

		// IPv6
		{name: "IPv6 loopback ::1", ip: "::1", want: true},
		{name: "IPv6 private fd00::1", ip: "fd00::1", want: true},
		{name: "IPv6 public 2001:4860:4860::8888", ip: "2001:4860:4860::8888", want: false},

		// CGNAT boundary (just outside the /10 range)
		{name: "CGNAT boundary 100.128.0.1", ip: "100.128.0.1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid IP address in test case: %s", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestCheckBlockedHostname(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		// Should be blocked
		{name: "localhost exact", host: "localhost", wantErr: true},
		{name: "metadata.google.internal", host: "metadata.google.internal", wantErr: true},
		{name: "metadata.svc.cluster.local", host: "metadata.svc.cluster.local", wantErr: true},
		{name: "evil.localhost suffix", host: "evil.localhost", wantErr: true},
		{name: "evil.local suffix", host: "evil.local", wantErr: true},
		{name: "evil.internal suffix", host: "evil.internal", wantErr: true},
		{name: "nip.io SSRF bypass", host: "evil.nip.io", wantErr: true},
		{name: "sslip.io SSRF bypass", host: "evil.sslip.io", wantErr: true},

		// Should NOT be blocked
		{name: "github.com", host: "github.com", wantErr: false},
		{name: "api.github.com", host: "api.github.com", wantErr: false},
		{name: "raw.githubusercontent.com", host: "raw.githubusercontent.com", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkBlockedHostname(tt.host)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("checkBlockedHostname(%q) = %v, wantErr = %v", tt.host, err, tt.wantErr)
			}
		})
	}
}

func TestSecureDialerControl(t *testing.T) {
	dialer := secureDialer()
	control := dialer.Control

	tests := []struct {
		name    string
		network string
		address string
		wantErr bool
	}{
		// Private IPs should be rejected by the Control callback
		{name: "block RFC1918 10.x", network: "tcp", address: "10.0.0.1:80", wantErr: true},
		{name: "block RFC1918 172.16.x", network: "tcp", address: "172.16.0.1:443", wantErr: true},
		{name: "block RFC1918 192.168.x", network: "tcp", address: "192.168.1.1:8080", wantErr: true},
		{name: "block loopback", network: "tcp", address: "127.0.0.1:22", wantErr: true},
		{name: "block link-local", network: "tcp", address: "169.254.169.254:80", wantErr: true},
		{name: "block CGNAT", network: "tcp", address: "100.64.0.1:443", wantErr: true},
		{name: "block IPv6 loopback", network: "tcp6", address: "[::1]:80", wantErr: true},
		{name: "block IPv6 private", network: "tcp6", address: "[fd00::1]:443", wantErr: true},

		// Public IPs should be allowed
		{name: "allow public IPv4", network: "tcp", address: "8.8.8.8:443", wantErr: false},
		{name: "allow public IPv6", network: "tcp6", address: "[2001:4860:4860::8888]:443", wantErr: false},
		{name: "allow github.com IP", network: "tcp", address: "140.82.121.4:443", wantErr: false},

		// CGNAT boundary
		{name: "allow CGNAT boundary", network: "tcp", address: "100.128.0.1:443", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := control(tt.network, tt.address, nil)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("Control(%q, %q, nil) = %v, wantErr = %v", tt.network, tt.address, err, tt.wantErr)
			}
		})
	}
}



