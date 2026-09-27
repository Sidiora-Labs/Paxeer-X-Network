package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func requiredEnv() map[string]string {
	return map[string]string{
		"GITHUB_REPOSITORY": "acme/widgets",
		"GITHUB_TOKEN":      testGitHubToken,
		"FLY_API_TOKEN":     testFlyToken,
		"RUNNER_IMAGE":      "registry.fly.io/paxeer-ci-runners:abc123",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(envOf(requiredEnv()))
	if err != nil {
		t.Fatal(err)
	}
	want := config{
		Owner:        "acme",
		Repo:         "widgets",
		GitHubToken:  testGitHubToken,
		FlyToken:     testFlyToken,
		RunnerApp:    "paxeer-ci-runners",
		RunnerImage:  "registry.fly.io/paxeer-ci-runners:abc123",
		Labels:       []string{"fly-linux"},
		Region:       "iad",
		CPUKind:      "performance",
		CPUs:         4,
		MemoryMB:     16384,
		MaxMachines:  24,
		PollInterval: 20 * time.Second,
		IdleTimeout:  900,
		OrphanTTL:    3 * time.Hour,
		GitHubAPIURL: "https://api.github.com",
		FlyAPIURL:    "https://api.machines.dev",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config = %+v\nwant   %+v", cfg, want)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	env := requiredEnv()
	env["FLY_RUNNER_APP"] = "other-runners"
	env["RUNNER_LABELS"] = " fly-linux , fly-big ,"
	env["FLY_REGION"] = "ord"
	env["MACHINE_CPU_KIND"] = "shared"
	env["MACHINE_CPUS"] = "8"
	env["MACHINE_MEMORY_MB"] = "32768"
	env["MAX_MACHINES"] = "3"
	env["POLL_INTERVAL"] = "5s"
	env["RUNNER_IDLE_TIMEOUT"] = "60"
	env["ORPHAN_TTL"] = "90m"
	env["GITHUB_API_URL"] = "http://127.0.0.1:8080/api/v3/"
	env["FLY_API_URL"] = "http://127.0.0.1:4280"
	cfg, err := loadConfig(envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RunnerApp != "other-runners" || strings.Join(cfg.Labels, ",") != "fly-linux,fly-big" || cfg.Region != "ord" ||
		cfg.CPUKind != "shared" || cfg.CPUs != 8 || cfg.MemoryMB != 32768 || cfg.MaxMachines != 3 ||
		cfg.PollInterval != 5*time.Second || cfg.IdleTimeout != 60 || cfg.OrphanTTL != 90*time.Minute ||
		cfg.GitHubAPIURL != "http://127.0.0.1:8080/api/v3" || cfg.FlyAPIURL != "http://127.0.0.1:4280" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadConfigNamesEachMissingRequiredValue(t *testing.T) {
	_, err := loadConfig(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_TOKEN", "FLY_API_TOKEN", "RUNNER_IMAGE"} {
		if !strings.Contains(err.Error(), name+" is required") {
			t.Errorf("error does not name %s: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), "FLY_REGION") {
		t.Errorf("error names a defaulted value: %v", err)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"GITHUB_REPOSITORY": "acme",
		"MACHINE_CPUS":      "0",
		"MAX_MACHINES":      "many",
		"POLL_INTERVAL":     "20",
		"ORPHAN_TTL":        "-1h",
		"RUNNER_LABELS":     " , ",
		"FLY_API_URL":       "api.machines.dev",
	}
	for name, value := range cases {
		env := requiredEnv()
		env[name] = value
		_, err := loadConfig(envOf(env))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%q: err = %v", name, value, err)
		}
	}
}
