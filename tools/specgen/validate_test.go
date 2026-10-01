package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseString(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := parse(strings.NewReader(src), "test.kvx")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

const testWorkflow = `[meta]
active_feature = "f"
[status_values]
task = ["pending", "in_progress", "done"]
`

func task(id, wave, requires, status string) string {
	return "[task." + id + "]\nstatus = \"" + status + "\"\nwave = " + wave + "\nrequires = [" + requires + "]\nreqs = [\"1\"]\ntouches = [\"a\"]\nverify_cmd = \"true\"\n"
}

func TestValidateAcceptsWellFormedGraph(t *testing.T) {
	wf := parseString(t, testWorkflow)
	spec := parseString(t, "[req.1]\nac_1 = \"x\"\n"+task("1", "1", "", "done")+task("2", "2", `"1"`, "pending"))
	if errs := Validate(wf, spec); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateRejectsDefects(t *testing.T) {
	wf := parseString(t, testWorkflow)
	cases := map[string]struct{ src, want string }{
		"unknown task":   {task("1", "1", `"9"`, "pending"), `requires unknown task "9"`},
		"forward wave":   {task("1", "1", `"2"`, "pending") + task("2", "2", "", "pending"), "from later wave"},
		"cycle":          {task("1", "1", `"2"`, "pending") + task("2", "1", `"1"`, "pending"), "dependency cycle"},
		"status":         {task("1", "1", "", "blocked"), `unsupported status "blocked"`},
		"unknown req":    {strings.Replace(task("1", "1", "", "pending"), `"1"]`+"\ntouches", `"7"]`+"\ntouches", 1), `unknown requirement "7"`},
		"no gate":        {strings.Replace(task("1", "1", "", "pending"), `verify_cmd = "true"`, `verify_cmd = ""`, 1), "no verify_cmd"},
		"no paths":       {strings.Replace(task("1", "1", "", "pending"), `touches = ["a"]`, `touches = []`, 1), "no exact paths"},
		"bad wave":       {task("1", "x", "", "pending"), "not a non-negative integer"},
		"bad resolution": {task("1", "1", "", "pending") + "[resolution.1]\napplies_to = [\"capability.404\"]\n", `applies_to unknown clause "capability.404"`},
	}
	for name, c := range cases {
		spec := parseString(t, "[req.1]\nac_1 = \"x\"\n"+c.src)
		errs := Validate(wf, spec)
		if !strings.Contains(strings.Join(errs, "\n"), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, errs)
		}
	}
}

func TestParseRejectsDuplicateKeys(t *testing.T) {
	if _, err := parse(strings.NewReader("[a]\nk = \"1\"\nk = \"2\"\n"), "dup.kvx"); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("want duplicate key error, got %v", err)
	}
}

// TestRepositorySpecIsValidAndCurrent validates the real unified spec and
// checks every generated pointer is current and names the active feature.
func TestRepositorySpecIsValidAndCurrent(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := ParseFile(filepath.Join(repo, "spec", "workflow.kvx"))
	if err != nil {
		t.Fatal(err)
	}
	active := wf.Str("meta", "active_feature")
	if active != "paxeer-x" {
		t.Fatalf("active feature = %q, want paxeer-x", active)
	}
	w := &writer{check: true}
	if err := run(repo, w); err != nil {
		t.Fatalf("run: %v", err)
	}
	if w.stale != 0 {
		t.Fatalf("%d generated output(s) stale", w.stale)
	}
	for _, kv := range wf.OrderedKV("adapters", "") {
		b, err := os.ReadFile(filepath.Join(repo, kv[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "spec/"+active+"/spec.kvx") {
			t.Errorf("%s does not point at the active feature", kv[1])
		}
		for _, private := range []string{"/root/", "lx-ops", ".env", "PRIVATE"} {
			if strings.Contains(string(b), private) {
				t.Errorf("%s contains private operational reference %q", kv[1], private)
			}
		}
	}
}
