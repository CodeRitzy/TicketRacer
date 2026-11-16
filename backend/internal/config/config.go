// Package config loads all runtime settings from environment variables.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every tunable. Secrets come only from the environment.
type Config struct {
	Addr, DatabaseURL, RedisURL, AdminToken string
	PaymentMode                             string // "demo" or "stripe"
	StripeSecretKey, StripeWebhookSecret    string
	DemoWebhookSecret                       string
	PublicURL, SelfURL                      string // browser-facing URL / loopback URL used by demo payments and the simulator
	HoldTTL, AdmittedTTL                    time.Duration
	MaxSeatsPerHold, AdmitPerSecond         int
	JoinRatePerMin                          int // per-IP limit on joining the waiting room
}

// LoadDotEnv copies KEY=VALUE lines from a file into the environment without overriding real env vars.
// A missing file is fine (Docker passes real env vars).
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		k = strings.TrimSpace(k)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, strings.TrimSpace(v))
		}
	}
}

func str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func integer(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

// Load reads the environment and rejects combinations that cannot work.
func Load() (Config, error) {
	c := Config{
		Addr:                str("ADDR", ":8081"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		RedisURL:            str("REDIS_URL", "redis://localhost:6379/0"),
		AdminToken:          os.Getenv("ADMIN_TOKEN"),
		PaymentMode:         str("PAYMENT_MODE", "demo"),
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		DemoWebhookSecret:   os.Getenv("DEMO_WEBHOOK_SECRET"),
		PublicURL:           strings.TrimRight(str("PUBLIC_URL", "http://localhost:8080"), "/"),
		SelfURL:             strings.TrimRight(str("SELF_URL", "http://127.0.0.1:8081"), "/"),
		HoldTTL:             time.Duration(integer("HOLD_TTL_SECONDS", 600)) * time.Second,
		AdmittedTTL:         time.Duration(integer("ADMITTED_TTL_SECONDS", 1800)) * time.Second,
		MaxSeatsPerHold:     integer("MAX_SEATS_PER_HOLD", 6),
		AdmitPerSecond:      integer("ADMIT_PER_SECOND", 50),
		JoinRatePerMin:      integer("JOIN_RATE_PER_MIN", 60),
	}
	switch {
	case c.DatabaseURL == "":
		return c, errors.New("DATABASE_URL is required")
	case c.PaymentMode == "demo" && c.DemoWebhookSecret == "":
		return c, errors.New("DEMO_WEBHOOK_SECRET is required in demo payment mode")
	case c.PaymentMode == "stripe" && (c.StripeSecretKey == "" || c.StripeWebhookSecret == ""):
		return c, errors.New("STRIPE_SECRET_KEY and STRIPE_WEBHOOK_SECRET are required in stripe payment mode")
	case c.PaymentMode != "demo" && c.PaymentMode != "stripe":
		return c, fmt.Errorf("PAYMENT_MODE must be demo or stripe, got %q", c.PaymentMode)
	}
	// a placeholder or short secret would let anyone forge payments or act as admin; refuse to start
	for name, v := range map[string]string{"ADMIN_TOKEN": c.AdminToken, "DEMO_WEBHOOK_SECRET": c.DemoWebhookSecret} {
		if v != "" && (len(v) < 24 || strings.Contains(v, "change-me")) {
			return c, fmt.Errorf("%s is a placeholder or shorter than 24 characters; set a random value", name)
		}
	}
	return c, nil
}
