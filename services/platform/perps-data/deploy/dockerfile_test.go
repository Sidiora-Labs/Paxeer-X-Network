package deploy

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type instruction struct {
	cmd  string
	args string
}

type stage struct {
	from         string
	instructions []instruction
}

var services = []string{"perps-indexer", "perps-gateway", "perps-explorer", "perps-portal", "perps-replay", "perps-loadtest"}

var (
	digestPattern = "*@sha256:" + strings.Repeat("?", 64) + ")"
	portVariable  = regexp.MustCompile(`^(\$PORT|\$\{PORT\})(/tcp)?$`)
	cgoSetting    = regexp.MustCompile(`CGO_ENABLED=(\S*)`)
)

func parseDockerfile(src string) ([]instruction, []stage, error) {
	var global []instruction
	var stages []stage
	var logical strings.Builder
	flush := func() error {
		line := strings.TrimSpace(logical.String())
		logical.Reset()
		if line == "" {
			return nil
		}
		cmd, args, _ := strings.Cut(line, " ")
		in := instruction{cmd: strings.ToUpper(cmd), args: strings.TrimSpace(args)}
		switch {
		case in.cmd == "FROM":
			stages = append(stages, stage{from: in.args})
		case len(stages) == 0 && in.cmd != "ARG":
			return fmt.Errorf("%s before the first FROM", in.cmd)
		case len(stages) == 0:
			global = append(global, in)
		default:
			last := &stages[len(stages)-1]
			last.instructions = append(last.instructions, in)
		}
		return nil
	}
	for _, raw := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(raw)
		if logical.Len() == 0 && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		if body, ok := strings.CutSuffix(trimmed, `\`); ok {
			logical.WriteString(body)
			logical.WriteString(" ")
			continue
		}
		logical.WriteString(trimmed)
		if err := flush(); err != nil {
			return nil, nil, err
		}
	}
	if logical.Len() != 0 {
		return nil, nil, errors.New("dangling line continuation")
	}
	return global, stages, nil
}

func (s stage) has(cmd, args string) bool {
	return slices.Contains(s.instructions, instruction{cmd: cmd, args: args})
}

func (s stage) last(cmd string) (string, bool) {
	for i := len(s.instructions) - 1; i >= 0; i-- {
		if s.instructions[i].cmd == cmd {
			return s.instructions[i].args, true
		}
	}
	return "", false
}

func (s stage) runContaining(fragment string) bool {
	for _, in := range s.instructions {
		if in.cmd == "RUN" && strings.Contains(in.args, fragment) {
			return true
		}
	}
	return false
}

func checkDockerfile(src string) error {
	global, stages, err := parseDockerfile(src)
	if err != nil {
		return err
	}
	wantGlobal := []instruction{{cmd: "ARG", args: "BUILD_IMAGE"}, {cmd: "ARG", args: "RUNTIME_IMAGE"}}
	if !slices.Equal(global, wantGlobal) {
		return fmt.Errorf("global ARGs %v, want BUILD_IMAGE and RUNTIME_IMAGE without defaults", global)
	}
	if len(stages) != 2 {
		return fmt.Errorf("%d stages, want build and runtime", len(stages))
	}
	build, runtime := stages[0], stages[1]
	if build.from != "${BUILD_IMAGE} AS build" {
		return fmt.Errorf("build stage FROM %q", build.from)
	}
	if runtime.from != "${RUNTIME_IMAGE} AS runtime" {
		return fmt.Errorf("runtime stage FROM %q", runtime.from)
	}
	for _, name := range []string{"BUILD_IMAGE", "RUNTIME_IMAGE"} {
		if !build.has("ARG", name) || !build.runContaining(`case "${`+name+`}" in `+digestPattern) {
			return fmt.Errorf("%s is not refused unless pinned by digest", name)
		}
	}
	for _, s := range stages {
		for _, in := range s.instructions {
			for _, m := range cgoSetting.FindAllStringSubmatch(in.args, -1) {
				if m[1] != "0" {
					return fmt.Errorf("CGO_ENABLED=%s in %s", m[1], in.cmd)
				}
			}
		}
	}
	env, ok := build.last("ENV")
	if !ok || !slices.Contains(strings.Fields(env), "CGO_ENABLED=0") || !slices.Contains(strings.Fields(env), "GOFLAGS=-mod=readonly") {
		return errors.New("build stage ENV does not set CGO_ENABLED=0 and GOFLAGS=-mod=readonly")
	}
	if !build.has("ARG", "SERVICE") {
		return errors.New("build stage has no ARG SERVICE")
	}
	if !build.runContaining(`case "${SERVICE}" in ` + strings.Join(services, "|") + `) ;;`) {
		return errors.New("SERVICE is not restricted to the six binaries")
	}
	if !build.runContaining(`go build -mod=readonly -trimpath `) || !build.runContaining(`-o /out/app "./services/platform/perps-data/cmd/${SERVICE}"`) {
		return errors.New("go build does not use -mod=readonly -trimpath on services/platform/perps-data/cmd/${SERVICE}")
	}
	if !runtime.has("COPY", "--from=build --chown=65532:65532 /out/app /app/app") {
		return errors.New("runtime stage does not copy /out/app to /app/app owned by 65532:65532")
	}
	if user, _ := runtime.last("USER"); user != "65532:65532" {
		return fmt.Errorf("runtime USER %q, want 65532:65532", user)
	}
	if entry, _ := runtime.last("ENTRYPOINT"); entry != `["/app/app"]` {
		return fmt.Errorf("runtime ENTRYPOINT %q, want [\"/app/app\"]", entry)
	}
	for _, s := range stages {
		for _, in := range s.instructions {
			if in.cmd != "EXPOSE" {
				continue
			}
			for _, port := range strings.Fields(in.args) {
				if !portVariable.MatchString(port) {
					return fmt.Errorf("fixed EXPOSE %q", port)
				}
			}
		}
	}
	return nil
}

func TestDockerfile(t *testing.T) {
	raw, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	src := string(raw)

	t.Run("dockerfile_shape", func(t *testing.T) {
		if err := checkDockerfile(src); err != nil {
			t.Fatalf("checkDockerfile: %v", err)
		}
	})

	t.Run("expose_port_variable_accepted", func(t *testing.T) {
		if err := checkDockerfile(src + "EXPOSE ${PORT}\n"); err != nil {
			t.Fatalf("checkDockerfile: %v", err)
		}
	})

	refusals := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{"fixed_expose", `ENTRYPOINT ["/app/app"]`, "EXPOSE 8080\n" + `ENTRYPOINT ["/app/app"]`, `fixed EXPOSE "8080"`},
		{"build_image_default", "ARG BUILD_IMAGE\nARG RUNTIME_IMAGE\nFROM", "ARG BUILD_IMAGE=golang:1.25.6\nARG RUNTIME_IMAGE\nFROM", "global ARGs [{ARG BUILD_IMAGE=golang:1.25.6} {ARG RUNTIME_IMAGE}], want BUILD_IMAGE and RUNTIME_IMAGE without defaults"},
		{"runtime_digest_unchecked", `case "${RUNTIME_IMAGE}" in`, `case "${RUNTIME_TAG}" in`, "RUNTIME_IMAGE is not refused unless pinned by digest"},
		{"cgo_enabled", "ENV CGO_ENABLED=0", "ENV CGO_ENABLED=1", "CGO_ENABLED=1 in ENV"},
		{"service_list", "|perps-loadtest)", ")", "SERVICE is not restricted to the six binaries"},
		{"no_trimpath", "-mod=readonly -trimpath ", "-mod=readonly ", "go build does not use -mod=readonly -trimpath on services/platform/perps-data/cmd/${SERVICE}"},
		{"root_user", "USER 65532:65532", "USER root", `runtime USER "root", want 65532:65532`},
		{"entrypoint", `ENTRYPOINT ["/app/app"]`, `ENTRYPOINT ["/bin/sh"]`, `runtime ENTRYPOINT "[\"/bin/sh\"]", want ["/app/app"]`},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(src, tc.old, tc.new, 1)
			if mutated == src {
				t.Fatalf("mutation %q not found in Dockerfile", tc.old)
			}
			err := checkDockerfile(mutated)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("checkDockerfile error %v, want %q", err, tc.want)
			}
		})
	}
}
