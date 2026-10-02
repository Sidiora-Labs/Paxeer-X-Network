package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPIURL = "https://api.github.com"
	defaultFlyAPIURL    = "https://api.machines.dev"
)

type config struct {
	Readiness *readinessConfig
	QualificationRoot     string
	QualificationContract string
	Owner                 string
	Repo                  string
	GitHubToken           string
	FlyToken              string
	RunnerApp             string
	RunnerImage           string
	Labels                []string
	Region                string
	CPUKind               string
	CPUs                  int
	MemoryMB              int
	MaxMachines           int
	PollInterval          time.Duration
	IdleTimeout           int
	OrphanTTL             time.Duration
	GitHubAPIURL          string
	FlyAPIURL             string
}

func loadConfig(getenv func(string) string) (config, error) {
	var errs []error
	value := func(name, fallback string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return fallback
	}
	required := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
		return v
	}
	positiveInt := func(name string, fallback int) int {
		raw := value(name, "")
		if raw == "" {
			return fallback
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer, got %q", name, raw))
			return fallback
		}
		return n
	}
	positiveDuration := func(name string, fallback time.Duration) time.Duration {
		raw := value(name, "")
		if raw == "" {
			return fallback
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive duration, got %q", name, raw))
			return fallback
		}
		return d
	}
	apiURL := func(name, fallback string) string {
		raw := strings.TrimRight(value(name, fallback), "/")
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be an absolute http or https URL, got %q", name, raw))
		}
		return raw
	}

	cfg := config{
		QualificationRoot:     value("QUALIFICATION_STATE_DIR", ""),
		QualificationContract: value("QUALIFICATION_CONTRACT_SHA256", ""),
		RunnerApp:             value("FLY_RUNNER_APP", "paxeer-ci-runners"),
		Region:                value("FLY_REGION", "iad"),
		CPUKind:               value("MACHINE_CPU_KIND", "performance"),
		CPUs:                  positiveInt("MACHINE_CPUS", 4),
		MemoryMB:              positiveInt("MACHINE_MEMORY_MB", 16384),
		MaxMachines:           positiveInt("MAX_MACHINES", 24),
		PollInterval:          positiveDuration("POLL_INTERVAL", 20*time.Second),
		IdleTimeout:           positiveInt("RUNNER_IDLE_TIMEOUT", 900),
		OrphanTTL:             positiveDuration("ORPHAN_TTL", 3*time.Hour),
		GitHubAPIURL:          apiURL("GITHUB_API_URL", defaultGitHubAPIURL),
		FlyAPIURL:             apiURL("FLY_API_URL", defaultFlyAPIURL),
	}

	if repository := required("GITHUB_REPOSITORY"); repository != "" {
		owner, repo, ok := strings.Cut(repository, "/")
		if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
			errs = append(errs, fmt.Errorf("GITHUB_REPOSITORY must be owner/repo, got %q", repository))
		}
		cfg.Owner, cfg.Repo = owner, repo
	}
	cfg.GitHubToken = required("GITHUB_TOKEN")
	cfg.FlyToken = required("FLY_API_TOKEN")
	cfg.RunnerImage = required("RUNNER_IMAGE")

	for _, label := range strings.Split(value("RUNNER_LABELS", "fly-linux"), ",") {
		if label = strings.TrimSpace(label); label != "" {
			cfg.Labels = append(cfg.Labels, label)
		}
	}
	if len(cfg.Labels) == 0 {
		errs = append(errs, errors.New("RUNNER_LABELS must name at least one label"))
	}

	if cfg.QualificationRoot != "" && !qDigest.MatchString(cfg.QualificationContract) {
		errs = append(errs, errors.New("QUALIFICATION_CONTRACT_SHA256 must pin the accepted complete contract"))
	}
	if len(errs) == 0 {
		readiness, err := loadReadinessConfig(strings.TrimSpace(getenv("CI_READINESS_CONFIG_FILE")), cfg)
		if err != nil { errs = append(errs, err) } else { cfg.Readiness = readiness }
	}
	return cfg, errors.Join(errs...)
}
