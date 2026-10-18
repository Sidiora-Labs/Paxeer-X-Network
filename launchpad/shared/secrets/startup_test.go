package secrets

import (
	"context"
	"testing"
	"time"
)

func TestProductionStartupRequiredSecrets(t *testing.T) {
	t.Setenv("L45_REQUIRED_KEY", "actual-environment-value")
	for _, provider := range []ProviderType{ProviderVault, ProviderAWS, "unknown"} {
		if _, err := LoadProduction(context.Background(), provider, time.Second, []string{"L45_REQUIRED_KEY"}); err == nil {
			t.Fatal("unsupported production provider accepted", provider)
		}
	}
	if _, err := LoadProduction(context.Background(), "", time.Second, []string{"L45_REQUIRED_KEY"}); err == nil {
		t.Fatal("implicit production provider accepted")
	}
	startup, err := LoadProduction(context.Background(), ProviderEnv, time.Second, []string{"L45_REQUIRED_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if err = startup.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("L45_REQUIRED_KEY", "rotated-environment-value")
	if startup.Ready(context.Background()) == nil {
		t.Fatal("stale consumed secret remained ready")
	}
}
