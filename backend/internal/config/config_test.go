package config

import (
	"strings"
	"testing"
)

// Placeholder or short secrets must stop the server from starting.
func TestLoadRejectsWeakSecrets(t *testing.T) {
	strong := strings.Repeat("x", 32)
	cases := map[string]struct {
		admin, demo, publicURL string
		ok                     bool
	}{
		"strong":            {strong, strong, "", true},
		"admin placeholder": {"change-me", strong, "", false},
		"demo placeholder":  {strong, "ci-change-me-xxxxxxxxxxxxxx", "", false},
		"short admin":       {"abc123", strong, "", false},
		"admin unset":       {"", strong, "", true}, // admin features simply stay disabled
		// the published local default runs on your own machine and nowhere else
		"local default on localhost": {LocalDefault, LocalDefault, "http://localhost:8080", true},
		"local default on loopback":  {LocalDefault, LocalDefault, "http://127.0.0.1:8080", true},
		"local default deployed":     {LocalDefault, strong, "https://tickets.example.com", false},
		"local default demo secret":  {strong, LocalDefault, "https://tickets.example.com", false},
		"strong secrets deployed":    {strong, strong, "https://tickets.example.com", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("PAYMENT_MODE", "demo")
			t.Setenv("PUBLIC_URL", tc.publicURL) // empty means the localhost default
			t.Setenv("ADMIN_TOKEN", tc.admin)
			t.Setenv("DEMO_WEBHOOK_SECRET", tc.demo)
			if _, err := Load(); (err == nil) != tc.ok {
				t.Fatalf("Load() err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
