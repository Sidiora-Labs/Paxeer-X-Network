package redis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	r "github.com/redis/go-redis/v9"
)

type PolicySnapshot struct {
	Generation       string          `json:"generation"`
	Revision         string          `json:"revision"`
	Visibility       string          `json:"visibility"`
	Class            string          `json:"class"`
	ContentDigest    string          `json:"contentDigest"`
	ProjectionDigest string          `json:"projectionDigest"`
	SourceWatermark  json.RawMessage `json:"sourceWatermark"`
	FetchedAt        time.Time       `json:"fetchedAt"`
	FreshUntil       time.Time       `json:"freshUntil"`
	RetainUntil      time.Time       `json:"retainUntil"`
	Body             json.RawMessage `json:"body"`
}

func CompareRevision(a, b string) (int, error) {
	if !revisionOK(a) || !revisionOK(b) {
		return 0, errors.New("cache: invalid revision")
	}
	if len(a) < len(b) {
		return -1, nil
	}
	if len(a) > len(b) {
		return 1, nil
	}
	if a < b {
		return -1, nil
	}
	if a > b {
		return 1, nil
	}
	return 0, nil
}

func (s PolicySnapshot) Validate() error {
	if s.Generation == "" || len(s.Generation) > 128 || !revisionOK(s.Revision) || s.FetchedAt.IsZero() || !s.FreshUntil.After(s.FetchedAt) || s.RetainUntil.Before(s.FreshUntil) || s.RetainUntil.Sub(s.FetchedAt) > 24*time.Hour || !json.Valid(s.Body) || !json.Valid(s.SourceWatermark) {
		return errors.New("cache: invalid policy snapshot")
	}
	if s.Visibility != "public" && s.Visibility != "private" {
		return errors.New("cache: invalid visibility")
	}
	if s.Class != "public_snapshot" && s.Class != "private_account" {
		return errors.New("cache: uncacheable class")
	}
	if (s.Class == "public_snapshot") != (s.Visibility == "public") {
		return errors.New("cache: class visibility conflict")
	}
	if s.Class != "public_snapshot" && !s.RetainUntil.Equal(s.FreshUntil) {
		return errors.New("cache: stale private snapshot prohibited")
	}
	b, e := json.Marshal(s)
	if e != nil || len(b) > MaxSnapshotBytes {
		return errors.New("cache: snapshot exceeds encoded bound")
	}
	return nil
}

func PolicySnapshotDigest(s PolicySnapshot) string {
	h := sha256.New()
	h.Write(s.Body)
	h.Write([]byte{0})
	h.Write(s.SourceWatermark)
	return hex.EncodeToString(h.Sum(nil))
}

// Projection identity excludes response trace, generation time and renewed cursor
// signatures. Exact data, quality, revision and source watermark remain bound.
func PolicyProjectionDigest(s PolicySnapshot) string {
	d := json.NewDecoder(bytes.NewReader(s.Body))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		delete(object, "requestId")
		delete(object, "generatedAt")
		delete(object, "nextCursor")
	}
	body, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	copy := s
	copy.Body = body
	return PolicySnapshotDigest(copy)
}

const putPolicySnapshot = `if ARGV[5]~='' and redis.call('GET',KEYS[2])~=ARGV[5] then return -2 end;local old=redis.call('GET',KEYS[1]);if old then local o=cjson.decode(old);if o.generation~=ARGV[2] then return -1 end;if o.class~=ARGV[7] or o.visibility~=ARGV[8] then return -4 end;local a=o.revision;local b=ARGV[3];if #a>#b or (#a==#b and a>b) then return 0 end;if a==b then if o.body==cjson.null then return 0 end;if o.projectionDigest~=ARGV[6] then return -3 end end end;redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[4]);return 1`

// Equal revision can refresh an unchanged authoritative projection; a revision
// floor cannot be resurrected. Lease-checked publication rejects expired work.
func PutPolicySnapshot(ctx context.Context, c *r.Client, key string, s PolicySnapshot, leaseKey, token string) (bool, error) {
	if c == nil || key == "" || len(key) > 512 || len(leaseKey) > 512 || (token != "" && (len(token) != 64 || leaseKey == "")) {
		return false, errors.New("cache: invalid policy destination")
	}
	s.ContentDigest = PolicySnapshotDigest(s)
	s.ProjectionDigest = PolicyProjectionDigest(s)
	if e := s.Validate(); e != nil {
		return false, e
	}
	ttl := time.Until(s.RetainUntil)
	if ttl < time.Millisecond || ttl > 24*time.Hour {
		return false, errors.New("cache: expired policy snapshot")
	}
	b, e := json.Marshal(s)
	if e != nil {
		return false, e
	}
	n, e := c.Eval(ctx, putPolicySnapshot, []string{key, leaseKey}, b, s.Generation, s.Revision, ttl.Milliseconds(), token, s.ProjectionDigest, s.Class, s.Visibility).Int()
	if e != nil {
		return false, e
	}
	if n < 0 {
		return false, errors.New("cache: generation or refresh ownership changed")
	}
	return n == 1, nil
}

func GetPolicySnapshot(ctx context.Context, c *r.Client, key string) (PolicySnapshot, bool, error) {
	var s PolicySnapshot
	if c == nil || key == "" || len(key) > 512 {
		return s, false, errors.New("cache: invalid policy source")
	}
	b, e := c.Get(ctx, key).Bytes()
	if errors.Is(e, r.Nil) {
		return s, false, nil
	}
	if e != nil {
		return s, false, e
	}
	if len(b) > MaxSnapshotBytes {
		return s, false, errors.New("cache: oversized policy snapshot")
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return s, false, e
	}
	if e = s.Validate(); e != nil {
		return s, false, e
	}
	if string(s.Body) != "null" && (s.ContentDigest != PolicySnapshotDigest(s) || s.ProjectionDigest != PolicyProjectionDigest(s)) {
		return s, false, errors.New("cache: representation digest mismatch")
	}
	return s, time.Now().Before(s.RetainUntil) && string(s.Body) != "null", nil
}

const invalidatePolicySnapshot = `local old=redis.call('GET',KEYS[1]);if old then local o=cjson.decode(old);if o.generation~=ARGV[1] then return 0 end;local a=o.revision;local b=ARGV[2];if #a>#b or (#a==#b and a>=b) then return 0 end end;redis.call('SET',KEYS[1],ARGV[3],'PX',ARGV[4]);return 1`

func InvalidatePolicySnapshot(ctx context.Context, c *r.Client, key string, floor PolicySnapshot) (bool, error) {
	floor.Body = json.RawMessage(`null`)
	floor.FetchedAt = time.Now()
	floor.FreshUntil = floor.FetchedAt.Add(InvalidationFloorTTL)
	floor.RetainUntil = floor.FreshUntil
	if c == nil || key == "" || len(key) > 512 {
		return false, errors.New("cache: invalid invalidation destination")
	}
	if e := floor.Validate(); e != nil {
		return false, e
	}
	b, e := json.Marshal(floor)
	if e != nil {
		return false, e
	}
	n, e := c.Eval(ctx, invalidatePolicySnapshot, []string{key}, floor.Generation, floor.Revision, b, InvalidationFloorTTL.Milliseconds()).Int()
	return n == 1, e
}
