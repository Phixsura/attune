package domain

import (
	"net"
	"testing"
	"time"
)

func TestExpiryToDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		expiry APIKeyExpiry
		want   time.Duration
	}{
		{"7 days", APIKeyExpiry7Days, 7 * 24 * time.Hour},
		{"30 days", APIKeyExpiry30Days, 30 * 24 * time.Hour},
		{"90 days", APIKeyExpiry90Days, 90 * 24 * time.Hour},
		{"1 year", APIKeyExpiry1Year, 365 * 24 * time.Hour},
		{"never", APIKeyExpiryNever, 0},
		{"custom", APIKeyExpiryCustom, 0},
		{"unknown", APIKeyExpiry("unknown"), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExpiryToDuration(tt.expiry); got != tt.want {
				t.Errorf("ExpiryToDuration(%q) = %v, want %v", tt.expiry, got, tt.want)
			}
		})
	}
}

func TestValidExpiries(t *testing.T) {
	t.Parallel()
	expiries := ValidExpiries()
	if len(expiries) != 5 {
		t.Errorf("ValidExpiries() returned %d items, want 5", len(expiries))
	}
}

func TestParseCIDRs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cidrs   []string
		wantLen int
		wantErr bool
	}{
		{"empty", nil, 0, false},
		{"single CIDR", []string{"10.0.0.0/8"}, 1, false},
		{"multiple CIDRs", []string{"10.0.0.0/8", "192.168.0.0/16"}, 2, false},
		{"single IP v4", []string{"192.168.1.1"}, 1, false},
		{"single IP v6", []string{"::1"}, 1, false},
		{"invalid CIDR", []string{"not-a-cidr"}, 0, true},
		{"mixed valid invalid", []string{"10.0.0.0/8", "invalid"}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCIDRs(tt.cidrs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCIDRs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("ParseCIDRs() returned %d nets, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestIPAllowed(t *testing.T) {
	t.Parallel()

	_, net10, _ := net.ParseCIDR("10.0.0.0/8")
	_, net192, _ := net.ParseCIDR("192.168.0.0/16")

	tests := []struct {
		name         string
		allowedCIDRs []*net.IPNet
		clientIP     net.IP
		want         bool
	}{
		{"empty allowlist allows all", nil, net.ParseIP("1.2.3.4"), true},
		{"nil IP denied", []*net.IPNet{net10}, nil, false},
		{"IP in range allowed", []*net.IPNet{net10}, net.ParseIP("10.1.2.3"), true},
		{"IP not in range denied", []*net.IPNet{net10}, net.ParseIP("192.168.1.1"), false},
		{"IP in second range allowed", []*net.IPNet{net10, net192}, net.ParseIP("192.168.1.1"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IPAllowed(tt.allowedCIDRs, tt.clientIP); got != tt.want {
				t.Errorf("IPAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAllowedStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		allowedCIDRs []string
		clientIP     string
		want         bool
	}{
		{"empty allowlist allows all", nil, "1.2.3.4", true},
		{"invalid client IP denied", []string{"10.0.0.0/8"}, "invalid", false},
		{"IP in CIDR range allowed", []string{"10.0.0.0/8"}, "10.1.2.3", true},
		{"IP not in CIDR range denied", []string{"10.0.0.0/8"}, "192.168.1.1", false},
		{"exact IP match allowed", []string{"192.168.1.1"}, "192.168.1.1", true},
		{"exact IP no match denied", []string{"192.168.1.1"}, "192.168.1.2", false},
		{"invalid CIDR skipped", []string{"invalid", "192.168.1.1"}, "192.168.1.1", true},
		{"invalid CIDR only denied", []string{"invalid"}, "192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IPAllowedStrings(tt.allowedCIDRs, tt.clientIP); got != tt.want {
				t.Errorf("IPAllowedStrings(%v, %q) = %v, want %v", tt.allowedCIDRs, tt.clientIP, got, tt.want)
			}
		})
	}
}

func TestValidEnvironments(t *testing.T) {
	t.Parallel()
	envs := ValidEnvironments()
	if len(envs) != 4 {
		t.Errorf("ValidEnvironments() returned %d items, want 4", len(envs))
	}
}

func TestIsValidEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  string
		want bool
	}{
		{"production", true},
		{"staging", true},
		{"development", true},
		{"test", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()
			if got := IsValidEnvironment(tt.env); got != tt.want {
				t.Errorf("IsValidEnvironment(%q) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
