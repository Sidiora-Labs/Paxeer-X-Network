package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const qualificationSchema = "paxeer-x.qualification-manifest.v1"
const qualificationLimit = 4 << 20

var qDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var qRevision = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var qSelector = regexp.MustCompile(`^[0-9]+(\.[0-9]+)+$`)
var qName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)

type qualificationThreshold struct {
	Operator string `json:"operator"`
	Value    int64  `json:"value"`
	Unit     string `json:"unit"`
}
type qualificationCell struct {
	ID             string                            `json:"id"`
	Selector       string                            `json:"selector"`
	GateIdentity   string                            `json:"gate_identity"`
	Acceptance     []string                          `json:"acceptance"`
	Matrix         map[string]string                 `json:"matrix"`
	Command        []string                          `json:"command"`
	MinimumSeconds int64                             `json:"minimum_seconds"`
	TimeoutSeconds int64                             `json:"timeout_seconds"`
	Thresholds     map[string]qualificationThreshold `json:"thresholds"`
	ArtifactKinds  []string                          `json:"artifact_kinds"`
	Runner         string                            `json:"runner"`
}
type qualificationManifest struct {
	Schema             string              `json:"schema"`
	CandidateSHA256    string              `json:"candidate_sha256"`
	Revision           string              `json:"revision"`
	Tree               string              `json:"tree"`
	EnvironmentSHA256  string              `json:"environment_sha256"`
	InputsSHA256       string              `json:"inputs_sha256"`
	ImagesSHA256       string              `json:"images_sha256"`
	WorkflowSHA256     string              `json:"workflow_sha256"`
	ContractSHA256     string              `json:"contract_sha256"`
	RequiredAcceptance []string            `json:"required_acceptance"`
	Cells              []qualificationCell `json:"cells"`
}
type qualificationRequest struct {
	Manifest  qualificationManifest `json:"manifest"`
	LogicalID string                `json:"logical_id"`
	Ref       string                `json:"ref"`
	Recipient string                `json:"recipient"`
}
type qualificationRecord struct {
	Request   qualificationRequest `json:"request"`
	Sequence  int64                `json:"sequence"`
	State     string               `json:"state"`
	RunID     int64                `json:"run_id"`
	Attempt   int64                `json:"attempt"`
	UpdatedAt time.Time            `json:"updated_at"`
	Previous  string               `json:"previous"`
	Detail    string               `json:"detail"`
}

func qHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func qCanonical(v any) []byte {
	b, _ := json.Marshal(v)
	var generic any
	_ = json.Unmarshal(b, &generic)
	b, _ = json.Marshal(generic)
	return b
}
func qStrict(b []byte, v any) error {
	if len(b) == 0 || len(b) > qualificationLimit {
		return errors.New("qualification document size refused")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return errors.New("duplicate field")
					}
					seen[s] = true
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			case '[':
				for d.More() {
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			default:
				return errors.New("invalid delimiter")
			}
		}
		return nil
	}
	if e := walk(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing data")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}
func (m qualificationManifest) validate() error {
	if m.Schema != qualificationSchema || !qRevision.MatchString(m.Revision) || !qRevision.MatchString(m.Tree) {
		return errors.New("candidate source invalid")
	}
	for _, s := range []string{m.CandidateSHA256, m.EnvironmentSHA256, m.InputsSHA256, m.ImagesSHA256, m.WorkflowSHA256, m.ContractSHA256} {
		if !qDigest.MatchString(s) {
			return errors.New("missing immutable binding")
		}
	}
	if len(m.Cells) == 0 || len(m.Cells) > 256 || len(m.RequiredAcceptance) == 0 {
		return errors.New("empty or oversized qualification coverage")
	}
	ids, coverage, required := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, a := range m.RequiredAcceptance {
		if a == "" || required[a] {
			return errors.New("invalid acceptance inventory")
		}
		required[a] = true
	}
	for _, c := range m.Cells {
		if !qName.MatchString(c.ID) || ids[c.ID] || !qSelector.MatchString(c.Selector) || !qDigest.MatchString(c.GateIdentity) {
			return errors.New("invalid cell identity")
		}
		ids[c.ID] = true
		if c.MinimumSeconds < 0 || c.TimeoutSeconds <= c.MinimumSeconds || c.TimeoutSeconds > 7*24*3600 {
			return errors.New("unsupported full-duration bound")
		}
		if c.Runner != "fly-linux" {
			return errors.New("unsupported runner profile; coverage cannot be dropped")
		}
		if len(c.Command) != 1 || c.Command[0] != "tools/paxeer-x/gates/"+c.Selector+".sh" {
			return errors.New("command must be the registered production gate")
		}
		if len(c.Acceptance) == 0 || len(c.ArtifactKinds) == 0 {
			return errors.New("incomplete case contract")
		}
		kinds := map[string]bool{}
		for _, k := range c.ArtifactKinds {
			if !qName.MatchString(k) || kinds[k] {
				return errors.New("invalid artifact contract")
			}
			kinds[k] = true
		}
		for _, a := range c.Acceptance {
			if !required[a] {
				return errors.New("unexpected acceptance")
			}
			coverage[a] = true
		}
		for k, v := range c.Matrix {
			if !qName.MatchString(k) || v == "" {
				return errors.New("invalid matrix")
			}
		}
		for k, b := range c.Thresholds {
			if !qName.MatchString(k) || !qName.MatchString(b.Unit) || (b.Operator != "min" && b.Operator != "max" && b.Operator != "eq") {
				return errors.New("invalid threshold")
			}
		}
	}
	if len(coverage) != len(required) {
		return errors.New("missing acceptance coverage")
	}
	return nil
}
func (r qualificationRequest) validate() error {
	if e := r.Manifest.validate(); e != nil {
		return e
	}
	if r.LogicalID != qHash(append([]byte("paxeer-x.logical.v1\x00"), qCanonical(r.Manifest)...)) {
		return errors.New("logical identity mismatch")
	}
	if !strings.HasPrefix(r.Ref, "refs/tags/") || strings.ContainsAny(r.Ref, " \n\r\t~^:?*[\\") {
		return errors.New("approved immutable tag ref required")
	}
	if _, e := qRecipient(r.Recipient); e != nil {
		return e
	}
	return nil
}
func qTransition(old, next string) bool {
	allowed := map[string][]string{"prepared": {"dispatch_intent"}, "dispatch_intent": {"acknowledgement_unknown", "run_bound", "terminal_failed"}, "acknowledgement_unknown": {"run_bound", "conflict"}, "run_bound": {"running", "completed_uncollected", "terminal_failed", "conflict"}, "running": {"completed_uncollected", "terminal_failed", "conflict"}, "completed_uncollected": {"completed_validated", "terminal_failed", "conflict"}}
	for _, s := range allowed[old] {
		if s == next {
			return true
		}
	}
	return false
}
