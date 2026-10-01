package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type qualificationEnvelope struct {
	Schema     string `json:"schema"`
	Key        []byte `json:"key"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Binding    string `json:"binding"`
}
type qualificationArtifact struct {
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
	Content []byte `json:"content"`
}
type qualificationResult struct {
	CellID                   string                  `json:"cell_id"`
	LogicalID                string                  `json:"logical_id"`
	CandidateSHA256          string                  `json:"candidate_sha256"`
	ManifestSHA256           string                  `json:"manifest_sha256"`
	Revision                 string                  `json:"revision"`
	Tree                     string                  `json:"tree"`
	EndRevision              string                  `json:"end_revision"`
	EndTree                  string                  `json:"end_tree"`
	Dirty                    bool                    `json:"dirty"`
	RunID                    int64                   `json:"run_id"`
	Attempt                  int64                   `json:"attempt"`
	ExitCode                 int                     `json:"exit_code"`
	Tests                    int                     `json:"tests"`
	Skipped                  int                     `json:"skipped"`
	StartedAt                time.Time               `json:"started_at"`
	CompletedAt              time.Time               `json:"completed_at"`
	UninterruptedNanoseconds int64                   `json:"uninterrupted_nanoseconds"`
	Measurements             map[string]int64        `json:"measurements"`
	Artifacts                []qualificationArtifact `json:"artifacts"`
}
type qualificationJobProof struct {
	ID          int64     `json:"id"`
	RunID       int64     `json:"run_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	RunnerID    int64     `json:"runner_id"`
	RunnerName  string    `json:"runner_name"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}
type qualificationCompleted struct {
	Schema         string                  `json:"schema"`
	Request        qualificationRequest    `json:"request"`
	RunID          int64                   `json:"run_id"`
	Attempt        int64                   `json:"attempt"`
	Repository     string                  `json:"repository"`
	Workflow       string                  `json:"workflow"`
	Status         string                  `json:"status"`
	Conclusion     string                  `json:"conclusion"`
	ObservedAt     time.Time               `json:"observed_at"`
	Domain         string                  `json:"domain"`
	Results        []qualificationResult   `json:"results"`
	Jobs           []qualificationJobProof `json:"jobs"`
	ArchiveDigests map[string]string       `json:"archive_digests"`
	Signature      []byte                  `json:"signature"`
}

func qRecipient(encoded string) (*rsa.PublicKey, error) {
	b, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return nil, e
	}
	k, e := x509.ParsePKIXPublicKey(b)
	if e != nil {
		return nil, e
	}
	r, ok := k.(*rsa.PublicKey)
	if !ok || r.N.BitLen() < 3072 {
		return nil, errors.New("RSA recipient below 3072 bits")
	}
	return r, nil
}
func qSeal(b []byte, binding, recipient string) (qualificationEnvelope, error) {
	var out qualificationEnvelope
	key, e := qRecipient(recipient)
	if e != nil {
		return out, e
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return out, e
	}
	block, e := aes.NewCipher(secret)
	if e != nil {
		return out, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return out, e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return out, e
	}
	wrapped, e := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, secret, []byte(binding))
	if e != nil {
		return out, e
	}
	return qualificationEnvelope{"paxeer-x.encrypted-artifact.v1", wrapped, nonce, gcm.Seal(nil, nonce, b, []byte(binding)), binding}, nil
}
func qOpen(e qualificationEnvelope, binding string, key *rsa.PrivateKey) ([]byte, error) {
	if e.Schema != "paxeer-x.encrypted-artifact.v1" || e.Binding != binding {
		return nil, errors.New("encrypted binding mismatch")
	}
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, e.Key, []byte(binding))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(e.Nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	return gcm.Open(nil, e.Nonce, e.Ciphertext, []byte(binding))
}
func qValidateResult(req qualificationRequest, result qualificationResult, run, attempt int64, job qualificationJobProof) error {
	var cell *qualificationCell
	for i := range req.Manifest.Cells {
		if req.Manifest.Cells[i].ID == result.CellID {
			cell = &req.Manifest.Cells[i]
		}
	}
	if cell == nil {
		return errors.New("unknown result cell")
	}
	m := req.Manifest
	if result.LogicalID != req.LogicalID || result.CandidateSHA256 != m.CandidateSHA256 || result.ManifestSHA256 != qHash(qCanonical(m)) || result.Revision != m.Revision || result.EndRevision != m.Revision || result.Tree != m.Tree || result.EndTree != m.Tree || result.Dirty {
		return errors.New("source or candidate mutation")
	}
	if result.RunID != run || result.Attempt != attempt || run <= 0 || attempt <= 0 || result.ExitCode != 0 || result.Tests <= 0 || result.Skipped != 0 {
		return errors.New("incomplete or failed result")
	}
	if job.RunID != run || job.ID <= 0 || job.Status != "completed" || job.Conclusion != "success" || job.RunnerID <= 0 || job.RunnerName == "" {
		return errors.New("job has no trusted completion")
	}
	duration := result.CompletedAt.Sub(result.StartedAt)
	if result.StartedAt.IsZero() || duration <= 0 || result.UninterruptedNanoseconds < cell.MinimumSeconds*int64(time.Second) || result.UninterruptedNanoseconds > int64(duration)+int64(time.Second) || duration > time.Duration(cell.TimeoutSeconds)*time.Second {
		return errors.New("uninterrupted duration refused")
	}
	if result.StartedAt.Before(job.StartedAt.Add(-time.Second)) || result.CompletedAt.After(job.CompletedAt.Add(time.Second)) {
		return errors.New("result outside job interval")
	}
	if len(result.Measurements) != len(cell.Thresholds) {
		return errors.New("threshold measurements incomplete")
	}
	for k, b := range cell.Thresholds {
		v, ok := result.Measurements[k]
		if !ok || (b.Operator == "min" && v < b.Value) || (b.Operator == "max" && v > b.Value) || (b.Operator == "eq" && v != b.Value) {
			return errors.New("threshold not met")
		}
	}
	kinds := map[string]bool{}
	for _, a := range result.Artifacts {
		if kinds[a.Kind] || len(a.Content) == 0 || qHash(a.Content) != a.SHA256 {
			return errors.New("mutated or repeated artifact")
		}
		kinds[a.Kind] = true
	}
	if len(kinds) != len(cell.ArtifactKinds) {
		return errors.New("artifact kinds mismatch")
	}
	for _, kind := range cell.ArtifactKinds {
		if !kinds[kind] {
			return errors.New("missing artifact")
		}
	}
	return nil
}
func qValidateCompleted(c qualificationCompleted, pub ed25519.PublicKey, domain string, deadline time.Time) error {
	if time.Now().After(deadline) {
		return errors.New("evidence deadline exceeded")
	}
	if e := c.Request.validate(); e != nil {
		return e
	}
	sig := c.Signature
	c.Signature = nil
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, qCanonical(c), sig) {
		return errors.New("untrusted completed record")
	}
	if c.Schema != "paxeer-x.completed-qualification.v1" || c.Domain != domain || domain == "" || c.Workflow != ".github/workflows/paxeer-x-qualification.yml" || c.Status != "completed" || c.Conclusion != "success" || c.Repository == "" || c.ObservedAt.IsZero() || c.ObservedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("completed record provenance invalid")
	}
	if len(c.Results) != len(c.Request.Manifest.Cells) || len(c.Jobs) != len(c.Results) || len(c.ArchiveDigests) != len(c.Results) {
		return errors.New("missing matrix cases")
	}
	seen := map[string]bool{}
	jobs := map[int64]bool{}
	for i, result := range c.Results {
		if time.Now().After(deadline) {
			return errors.New("evidence deadline exceeded")
		}
		if seen[result.CellID] || jobs[c.Jobs[i].ID] {
			return errors.New("duplicate case or job")
		}
		seen[result.CellID] = true
		jobs[c.Jobs[i].ID] = true
		if !qDigest.MatchString(c.ArchiveDigests[result.CellID]) {
			return errors.New("missing artifact provenance")
		}
		if e := qValidateResult(c.Request, result, c.RunID, c.Attempt, c.Jobs[i]); e != nil {
			return e
		}
	}
	return nil
}
func qSign(c *qualificationCompleted, key ed25519.PrivateKey) {
	c.Signature = nil
	c.Signature = ed25519.Sign(key, qCanonical(*c))
}
func qLoadKeys(root string) (*rsa.PrivateKey, ed25519.PrivateKey, error) {
	b, e := qRead(filepath.Join(root, "artifact-key.der"))
	if e != nil {
		return nil, nil, e
	}
	key, e := x509.ParsePKCS1PrivateKey(b)
	if e != nil || key.N.BitLen() < 3072 {
		return nil, nil, errors.New("invalid artifact decryption key")
	}
	b, e = qRead(filepath.Join(root, "signing-key"))
	if e != nil {
		return nil, nil, e
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("invalid signing key")
	}
	return key, ed25519.PrivateKey(b), nil
}
func qVerifyFile(path, trust string, deadline time.Time) error {
	b, e := qRead(path)
	if e != nil {
		return e
	}
	var completed qualificationCompleted
	if e = qStrict(b, &completed); e != nil {
		return e
	}
	key, e := qRead(trust)
	if e != nil {
		return e
	}
	return qValidateCompleted(completed, ed25519.PublicKey(key), "github-actions", deadline)
}
func qMounted(root string) bool {
	b, e := os.ReadFile("/proc/self/mountinfo")
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[4] == filepath.Dir(root) {
			return true
		}
	}
	return false
}
