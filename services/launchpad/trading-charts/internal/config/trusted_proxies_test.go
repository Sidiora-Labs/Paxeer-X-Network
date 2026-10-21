package config_test

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/config"
)

func TestTrustedProxies(t *testing.T) {
	c := config.Config{TrustedProxyCIDRs: " 10.0.0.0/8 , fd12::/16,"}
	c.NodeEnv = "development"
	got, err := c.TrustedProxies()
	if err != nil || len(got) != 2 || got[0].String() != "10.0.0.0/8" || got[1].String() != "fd12::/16" {
		t.Fatalf("got %v %v", got, err)
	}
	c.TrustedProxyCIDRs = ""
	if got, err = c.TrustedProxies(); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	c.TrustedProxyCIDRs = "10.0.0.1"
	if _, err = c.TrustedProxies(); err == nil {
		t.Fatal("bare address accepted")
	}
	c.TrustedProxyCIDRs = strings.Repeat("10.0.0.0/8,", 33)
	if _, err = c.TrustedProxies(); err == nil {
		t.Fatal("unbounded list accepted")
	}
	// Production never trusts forwarded client addresses.
	c.NodeEnv = "production"
	c.TrustedProxyCIDRs = "0.0.0.0/0"
	if got, err = c.TrustedProxies(); err != nil || got != nil {
		t.Fatalf("production: %v %v", got, err)
	}
}
