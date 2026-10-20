package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// RailwayConfig is one Railway service that runs a pre-built GHCR image.
// The rollout tool fills Image.Digest; the checked-in file leaves it empty.
type RailwayConfig struct {
	Service   string        `json:"service"`
	Image     RailwayImage  `json:"image"`
	Port      string        `json:"port"`
	Variables []string      `json:"variables"`
	Deploy    RailwayDeploy `json:"deploy"`
}

// RailwayImage is the GHCR repository, the services.json name of the service
// that tools/deploy/offchain-images.sh builds, and the digest placeholder.
type RailwayImage struct {
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

// RailwayDeploy is the Railway deploy section.
type RailwayDeploy struct {
	HealthcheckPath    string `json:"healthcheckPath"`
	HealthcheckTimeout int    `json:"healthcheckTimeout"`
	RestartPolicyType  string `json:"restartPolicyType"`
	NumReplicas        int    `json:"numReplicas"`
}

const (
	railwayDir     = "railway"
	projectFile    = "../../../../deploy/railway-perps-project.kvx"
	configPrefix   = "services/platform/perps-data/deploy/"
	livePath       = "/healthz/live"
	indexerService = "perps-data-indexer"
)

func decodeRailway(raw []byte) (*RailwayConfig, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c RailwayConfig
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the config")
	}
	return &c, nil
}

// railwayServices are the long-running image services of services.json,
// keyed by their SERVICE build argument.
func railwayServices(s *Services) map[string]Service {
	out := map[string]Service{}
	for _, svc := range s.Services {
		if svc.Run == "service" && svc.Build != nil && svc.Build.Service != "" {
			out[svc.Build.Service] = svc
		}
	}
	return out
}

func checkRailway(file string, c *RailwayConfig, svc Service) error {
	if c.Service != svc.Name {
		return fmt.Errorf("%s: service %q, want %q", file, c.Service, svc.Name)
	}
	if c.Image.Repository != svc.Name {
		return fmt.Errorf("%s: image repository %q, want %q", file, c.Image.Repository, svc.Name)
	}
	if c.Image.Digest != "" {
		return fmt.Errorf("%s: image digest %q must stay empty for the rollout tool", file, c.Image.Digest)
	}
	if _, ok := svc.Variables[c.Port]; c.Port != "PORT" || !ok {
		return fmt.Errorf("%s: port variable %q, want PORT declared in services.json", file, c.Port)
	}
	if want := slices.Sorted(maps.Keys(svc.Variables)); !slices.Equal(c.Variables, want) {
		return fmt.Errorf("%s: variables %v, want %v", file, c.Variables, want)
	}
	if c.Deploy.NumReplicas != svc.Replicas {
		return fmt.Errorf("%s: numReplicas %d, want %d", file, c.Deploy.NumReplicas, svc.Replicas)
	}
	if svc.Restart != "always" || c.Deploy.RestartPolicyType != "ALWAYS" {
		return fmt.Errorf("%s: restartPolicyType %q for restart %q, want ALWAYS for always", file, c.Deploy.RestartPolicyType, svc.Restart)
	}
	if c.Deploy.HealthcheckTimeout != 300 {
		return fmt.Errorf("%s: healthcheckTimeout %d, want 300", file, c.Deploy.HealthcheckTimeout)
	}
	if svc.Healthcheck.Kind != "http" || svc.Healthcheck.Path != readyPath {
		return fmt.Errorf("%s: services.json healthcheck %+v, want http %s", file, svc.Healthcheck, readyPath)
	}
	want := svc.Healthcheck.Path
	if svc.Name == indexerService {
		want = livePath
	}
	if c.Deploy.HealthcheckPath != want {
		return fmt.Errorf("%s: healthcheckPath %q, want %q", file, c.Deploy.HealthcheckPath, want)
	}
	return nil
}

func TestRailwayConfig(t *testing.T) {
	raw, err := os.ReadFile("services.json")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := decodeServices(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := railwayServices(layout)
	if len(want) != 4 {
		t.Fatalf("services.json has %d long-running image services, want 4", len(want))
	}

	paths, err := filepath.Glob(filepath.Join(railwayDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range paths {
		got = append(got, strings.TrimSuffix(filepath.Base(p), ".json"))
	}
	if wantNames := slices.Sorted(maps.Keys(want)); !slices.Equal(got, wantNames) {
		t.Fatalf("railway files %v, want %v", got, wantNames)
	}

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		c, err := decodeRailway(raw)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if err := checkRailway(p, c, want[name]); err != nil {
			t.Error(err)
		}
		if err := checkNoHostData(raw); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}

	t.Run("project images match", func(t *testing.T) {
		raw, err := os.ReadFile(projectFile)
		if err != nil {
			t.Fatal(err)
		}
		images, err := projectImages(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			image, ok := images[configPrefix+filepath.ToSlash(p)]
			if !ok {
				t.Errorf("%s names no service with config %s", projectFile, p)
				continue
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			c, err := decodeRailway(raw)
			if err != nil {
				t.Fatal(err)
			}
			if c.Image.Repository != image {
				t.Errorf("%s: image repository %q, %s pulls %q", p, c.Image.Repository, projectFile, image)
			}
		}
	})

	t.Run("root healthcheck path fails", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(railwayDir, "perps-gateway.json"))
		if err != nil {
			t.Fatal(err)
		}
		c, err := decodeRailway(raw)
		if err != nil {
			t.Fatal(err)
		}
		c.Deploy.HealthcheckPath = "/"
		if err := checkRailway("perps-gateway.json", c, want["perps-gateway"]); err == nil {
			t.Fatal("healthcheckPath / accepted")
		}
	})
}

// projectImages maps the config of every [service.*] section of the Railway
// project file to its image.
func projectImages(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	var image, config string
	flush := func() error {
		if config == "" {
			return nil
		}
		if image == "" {
			return fmt.Errorf("service with config %s has no image", config)
		}
		out[config] = image
		return nil
	}
	inService := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			if err := flush(); err != nil {
				return nil, err
			}
			image, config, inService = "", "", strings.HasPrefix(line, "[service.")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !inService || !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "image":
			image = strings.Trim(strings.TrimSpace(value), `"`)
		case "config":
			config = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return out, flush()
}
