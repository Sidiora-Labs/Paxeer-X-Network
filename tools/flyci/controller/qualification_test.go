package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var qTestOnce sync.Once
var qTestRSA *rsa.PrivateKey
var qTestSign ed25519.PrivateKey

func qTestKeys(t *testing.T) (*rsa.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	qTestOnce.Do(func() {
		var e error
		qTestRSA, e = rsa.GenerateKey(rand.Reader, 3072)
		if e != nil {
			panic(e)
		}
		_, qTestSign, e = ed25519.GenerateKey(rand.Reader)
		if e != nil {
			panic(e)
		}
	})
	return qTestRSA, qTestSign
}
func qTestRequest(t *testing.T) qualificationRequest {
	t.Helper()
	key, _ := qTestKeys(t)
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	hash := strings.Repeat("a", 64)
	m := qualificationManifest{Schema: qualificationSchema, CandidateSHA256: hash, Revision: strings.Repeat("b", 40), Tree: strings.Repeat("c", 40), EnvironmentSHA256: hash, InputsSHA256: hash, ImagesSHA256: hash, WorkflowSHA256: hash, ContractSHA256: hash, RequiredAcceptance: []string{"14.1"}, Cells: []qualificationCell{{ID: "duration", Selector: "0.6", GateIdentity: hash, Acceptance: []string{"14.1"}, Matrix: map[string]string{"language": "go"}, Command: []string{"tools/paxeer-x/gates/0.6.sh"}, MinimumSeconds: 60, TimeoutSeconds: 120, Thresholds: map[string]qualificationThreshold{"accepted": {Operator: "min", Value: 1, Unit: "count"}}, ArtifactKinds: []string{"result"}, Runner: "fly-linux"}}}
	return qualificationRequest{Manifest: m, LogicalID: qHash(append([]byte("paxeer-x.logical.v1\x00"), qCanonical(m)...)), Ref: "refs/tags/qualification-test", Recipient: base64.StdEncoding.EncodeToString(pub)}
}
func qTestRegistry(t *testing.T) (*qualificationRegistry, qualificationRequest) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	r, e := newQualificationRegistry(root)
	if e != nil {
		t.Fatal(e)
	}
	return r, qTestRequest(t)
}
func qTestComplete(t *testing.T) qualificationCompleted {
	t.Helper()
	req := qTestRequest(t)
	now := time.Now().UTC().Add(-time.Second)
	b := []byte("actual local artifact bytes; no provider execution claim")
	result := qualificationResult{CellID: "duration", LogicalID: req.LogicalID, CandidateSHA256: req.Manifest.CandidateSHA256, ManifestSHA256: qHash(qCanonical(req.Manifest)), Revision: req.Manifest.Revision, Tree: req.Manifest.Tree, EndRevision: req.Manifest.Revision, EndTree: req.Manifest.Tree, RunID: 1, Attempt: 1, Tests: 1, StartedAt: now.Add(-61 * time.Second), CompletedAt: now, UninterruptedNanoseconds: int64(61 * time.Second), Measurements: map[string]int64{"accepted": 1}, Artifacts: []qualificationArtifact{{"result", qHash(b), b}}}
	job := qualificationJobProof{ID: 2, RunID: 1, Name: "qualification-duration", Status: "completed", Conclusion: "success", RunnerID: 3, RunnerName: "local-harness", StartedAt: now.Add(-62 * time.Second), CompletedAt: now.Add(time.Second)}
	out := qualificationCompleted{Schema: "paxeer-x.completed-qualification.v1", Request: req, RunID: 1, Attempt: 1, Repository: "local/harness", Workflow: ".github/workflows/paxeer-x-qualification.yml", Status: "completed", Conclusion: "success", ObservedAt: now, Domain: "local-harness", Results: []qualificationResult{result}, Jobs: []qualificationJobProof{job}, ArchiveDigests: map[string]string{"duration": qHash(b)}}
	_, key := qTestKeys(t)
	qSign(&out, key)
	return out
}
func TestDurableQualificationRegistry(t *testing.T) {
	r, req := qTestRegistry(t)
	rec, e := r.submit(req)
	if e != nil || rec.State != "prepared" {
		t.Fatal(rec, e)
	}
	again, e := r.submit(req)
	if e != nil || again.Sequence != 1 {
		t.Fatal(again, e)
	}
	changed := req
	changed.Ref = "refs/tags/another"
	if _, e = r.submit(changed); e == nil {
		t.Fatal("changed logical request accepted")
	}
	if _, e = r.advance(req.LogicalID, "prepared", "dispatch_intent", 0, 0, ""); e != nil {
		t.Fatal(e)
	}
	reopened, e := newQualificationRegistry(r.root)
	if e != nil {
		t.Fatal(e)
	}
	rec, e = reopened.read(req.LogicalID)
	if e != nil || rec.State != "dispatch_intent" {
		t.Fatal(rec, e)
	}
	if _, e = reopened.advance(req.LogicalID, "dispatch_intent", "acknowledgement_unknown", 0, 0, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = reopened.advance(req.LogicalID, "acknowledgement_unknown", "dispatch_intent", 0, 0, ""); e == nil {
		t.Fatal("unknown acknowledgement redispatched")
	}
	if _, e = reopened.advance(req.LogicalID, "acknowledgement_unknown", "run_bound", 9, 1, ""); e != nil {
		t.Fatal(e)
	}
}
func TestQualificationProcessHelper(t *testing.T) {
	if os.Getenv("Q_REGISTRY_CHILD") != "1" {
		return
	}
	root := os.Getenv("Q_REGISTRY_ROOT")
	b, e := qRead(filepath.Join(root, "request.json"))
	if e != nil {
		os.Exit(10)
	}
	var req qualificationRequest
	if qStrict(b, &req) != nil {
		os.Exit(11)
	}
	r, e := newQualificationRegistry(root)
	if e != nil {
		os.Exit(12)
	}
	if _, e = r.submit(req); e != nil {
		os.Exit(13)
	}
	if _, e = r.advance(req.LogicalID, "prepared", "dispatch_intent", 0, 0, ""); e == nil {
		os.Exit(0)
	}
	os.Exit(17)
}
func TestDurableQualificationConcurrentProcesses(t *testing.T) {
	r, req := qTestRegistry(t)
	if e := qWrite(filepath.Join(r.root, "request.json"), qCanonical(req)); e != nil {
		t.Fatal(e)
	}
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestQualificationProcessHelper$")
		commands[i].Env = append(os.Environ(), "Q_REGISTRY_CHILD=1", "Q_REGISTRY_ROOT="+r.root)
		if e := commands[i].Start(); e != nil {
			t.Fatal(e)
		}
	}
	winners := 0
	for _, cmd := range commands {
		e := cmd.Wait()
		if e == nil {
			winners++
		} else if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
			t.Fatal(e)
		}
	}
	if winners != 1 {
		t.Fatalf("dispatch grants=%d", winners)
	}
	rec, e := r.read(req.LogicalID)
	if e != nil || rec.Sequence != 2 {
		t.Fatal(rec, e)
	}
}
func TestDurableQualificationCorruption(t *testing.T) {
	r, req := qTestRegistry(t)
	if _, e := r.submit(req); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(r.root, req.LogicalID, "00000000000000000001.json")
	if e := os.WriteFile(path, []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := r.read(req.LogicalID); e == nil {
		t.Fatal("corrupt registry accepted")
	}
	if _, e := r.submit(req); e == nil {
		t.Fatal("corrupt request recreated")
	}
}
func TestDurableQualificationManifest(t *testing.T) {
	req := qTestRequest(t)
	if e := req.validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*qualificationManifest){func(m *qualificationManifest) { m.Cells = nil }, func(m *qualificationManifest) { m.Cells = append(m.Cells, m.Cells[0]) }, func(m *qualificationManifest) { m.RequiredAcceptance = append(m.RequiredAcceptance, "14.2") }, func(m *qualificationManifest) { m.Cells[0].MinimumSeconds = 1000000 }, func(m *qualificationManifest) { m.Cells[0].Runner = "macos" }, func(m *qualificationManifest) { m.Cells[0].Command = []string{"sh", "-c", "true"} }} {
		var m qualificationManifest
		_ = json.Unmarshal(qCanonical(req.Manifest), &m)
		change(&m)
		if m.validate() == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	var v qualificationRequest
	if qStrict([]byte(`{"ref":"a","ref":"b"}`), &v) == nil {
		t.Fatal("duplicate JSON accepted")
	}
}
func TestDurableQualificationCrypto(t *testing.T) {
	key, signer := qTestKeys(t)
	req := qTestRequest(t)
	plain := []byte("private real artifact bytes")
	sealed, e := qSeal(plain, "binding", req.Recipient)
	if e != nil {
		t.Fatal(e)
	}
	opened, e := qOpen(sealed, "binding", key)
	if e != nil || string(opened) != string(plain) {
		t.Fatal(e)
	}
	if _, e = qOpen(sealed, "other", key); e == nil {
		t.Fatal("wrong binding accepted")
	}
	sealed.Ciphertext[0] ^= 1
	if _, e = qOpen(sealed, "binding", key); e == nil {
		t.Fatal("mutated ciphertext accepted")
	}
	completed := qTestComplete(t)
	if e = qValidateCompleted(completed, signer.Public().(ed25519.PublicKey), "local-harness", time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = qValidateCompleted(completed, signer.Public().(ed25519.PublicKey), "github-actions", time.Now().Add(time.Second)); e == nil {
		t.Fatal("harness claimed CI execution")
	}
	wrong, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	if qValidateCompleted(completed, wrong, "local-harness", time.Now().Add(time.Second)) == nil {
		t.Fatal("wrong signer accepted")
	}
}
func TestDurableQualificationEvidenceRefusals(t *testing.T) {
	_, key := qTestKeys(t)
	changes := map[string]func(*qualificationCompleted){"missing-cell": func(c *qualificationCompleted) { c.Results = nil }, "candidate": func(c *qualificationCompleted) { c.Results[0].CandidateSHA256 = strings.Repeat("0", 64) }, "short": func(c *qualificationCompleted) { c.Results[0].UninterruptedNanoseconds = int64(time.Second) }, "mutated-artifact": func(c *qualificationCompleted) { c.Results[0].Artifacts[0].Content = []byte("changed") }, "skipped": func(c *qualificationCompleted) { c.Results[0].Skipped = 1 }, "failed": func(c *qualificationCompleted) { c.Conclusion = "failure" }, "wrong-attempt": func(c *qualificationCompleted) { c.Results[0].Attempt = 2 }, "source-mutation": func(c *qualificationCompleted) { c.Results[0].Dirty = true }, "threshold": func(c *qualificationCompleted) { c.Results[0].Measurements["accepted"] = 0 }, "pending-job": func(c *qualificationCompleted) { c.Jobs[0].Status = "in_progress" }, "duration-outside-job": func(c *qualificationCompleted) { c.Results[0].StartedAt = c.Results[0].StartedAt.Add(-time.Hour) }}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := qTestComplete(t)
			change(&c)
			qSign(&c, key)
			if qValidateCompleted(c, key.Public().(ed25519.PublicKey), "local-harness", time.Now().Add(time.Second)) == nil {
				t.Fatal("invalid completed evidence accepted")
			}
		})
	}
	c := qTestComplete(t)
	if qValidateCompleted(c, key.Public().(ed25519.PublicKey), "local-harness", time.Now().Add(-time.Second)) == nil {
		t.Fatal("deadline ignored")
	}
}
func TestDurableQualificationPrivateFiles(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	path := filepath.Join(root, "artifact")
	if e := qWrite(path, []byte("bytes")); e != nil {
		t.Fatal(e)
	}
	if _, e := qRead(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(path, filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := qRead(path); e == nil {
		t.Fatal("hardlink accepted")
	}
	_ = os.Remove(filepath.Join(root, "link"))
	_ = os.Chmod(path, 0644)
	if _, e := qRead(path); e == nil {
		t.Fatal("public evidence accepted")
	}
	_ = os.Chmod(path, 0600)
	_ = os.Symlink(path, filepath.Join(root, "symlink"))
	if _, e := qRead(filepath.Join(root, "symlink")); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestDurableQualificationReadOnlyControl(t *testing.T) {
	r, req := qTestRegistry(t)
	if _, e := r.submit(req); e != nil {
		t.Fatal(e)
	}
	for _, operation := range []string{"status", "export"} {
		out := qHandle(r, qJSON(qualificationControl{Operation: operation, LogicalID: req.LogicalID}))
		if operation == "status" && out.Error != "" {
			t.Fatal(out.Error)
		}
		if operation == "export" && out.Error == "" {
			t.Fatal("pending export accepted")
		}
	}
	after, e := r.read(req.LogicalID)
	if e != nil || after.Sequence != 1 || after.State != "prepared" {
		t.Fatal("read operation dispatched", e)
	}
}
func TestDurableQualificationActualAssignmentLifetime(t *testing.T) {
	r, req := qTestRegistry(t)
	_, _ = r.submit(req)
	_, _ = r.advance(req.LogicalID, "prepared", "dispatch_intent", 0, 0, "")
	_, _ = r.advance(req.LogicalID, "dispatch_intent", "run_bound", 100, 1, "")
	idx := &jobIndex{byID: map[int64]workflowJob{22: {ID: 22, RunID: 100, RunnerID: 10, RunnerName: "fly-11", Status: "in_progress"}}, byRun: map[int64][]workflowJob{}}
	m := machine{Name: "fly-11", State: "started", CreatedAt: time.Now().Add(-48 * time.Hour)}
	rec := &reconciler{cfg: config{OrphanTTL: 3 * time.Hour}, qualification: r, now: time.Now}
	if got := rec.destroyReason(context.Background(), m, 11, 99, idx); got != "" {
		t.Fatal("active actual assignment removed:", got)
	}
	idx.byID = map[int64]workflowJob{}
	if got := rec.destroyReason(context.Background(), m, 11, 99, idx); got != "" {
		t.Fatal("unknown assignment removed:", got)
	}
}
func TestDurableQualificationEncryptedLogTransport(t *testing.T) {
	req := qTestRequest(t)
	key, _ := qTestKeys(t)
	plain := qCanonical(qTestComplete(t).Results[0])
	binding := req.LogicalID + "-duration-1"
	sealed, e := qSeal(plain, binding, req.Recipient)
	if e != nil {
		t.Fatal(e)
	}
	payload := qCanonical(sealed)
	encoded := base64.StdEncoding.EncodeToString(payload)
	mid := len(encoded) / 2
	line1 := "2026-10-01T00:00:00Z PAXEER_X_ENCRYPTED_V1 " + binding + " 0 2 " + qHash(payload) + " " + encoded[:mid] + "\n"
	line2 := "2026-10-01T00:00:01Z PAXEER_X_ENCRYPTED_V1 " + binding + " 1 2 " + qHash(payload) + " " + encoded[mid:] + "\n"
	got, e := qLogEnvelope([]byte(line1+line2), binding)
	if e != nil {
		t.Fatal(e)
	}
	var envelope qualificationEnvelope
	if e = qStrict(got, &envelope); e != nil {
		t.Fatal(e)
	}
	decrypted, e := qOpen(envelope, binding, key)
	if e != nil || string(decrypted) != string(plain) {
		t.Fatal("encrypted log did not authenticate", e)
	}
	for _, bad := range []string{line1, line1 + line1 + line2, strings.Replace(line1+line2, qHash(payload), strings.Repeat("0", 64), -1), strings.Replace(line1+line2, binding, "wrong", 1)} {
		if _, e = qLogEnvelope([]byte(bad), binding); e == nil {
			t.Fatal("truncated duplicate mutated or wrong envelope accepted")
		}
	}
}
func TestDurableQualificationFullDurationPreserved(t *testing.T) {
	req := qTestRequest(t)
	req.Manifest.Cells[0].MinimumSeconds = 24 * 3600
	req.Manifest.Cells[0].TimeoutSeconds = 25 * 3600
	before := req.Manifest.Cells[0].MinimumSeconds
	if e := req.Manifest.validate(); e != nil {
		t.Fatal(e)
	}
	if req.Manifest.Cells[0].MinimumSeconds != before {
		t.Fatal("duration shortened")
	}
}
