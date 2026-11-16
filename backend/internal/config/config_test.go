package config

import (
	"strings"
	"testing"
)

// Placeholder or short secrets must stop the server from starting.
func TestLoadRejectsWeakSecrets(t *testing.T) {
	strong := strings.Repeat("x", 32)
	cases := map[string]struct {
		admin, demo string
		ok          bool
	}{
		"strong":            {strong, strong, true},
		"admin placeholder": {"change-me", strong, false},
		"demo placeholder":  {strong, "ci-change-me-xxxxxxxxxxxxxx", false},
		"short admin":       {"abc123", strong, false},
		"admin unset":       {"", strong, true}, // admin features simply stay disabled
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("PAYMENT_MODE", "demo")
			t.Setenv("ADMIN_TOKEN", tc.admin)
			t.Setenv("DEMO_WEBHOOK_SECRET", tc.demo)
			if _, err := Load(); (err == nil) != tc.ok {
				t.Fatalf("Load() err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
