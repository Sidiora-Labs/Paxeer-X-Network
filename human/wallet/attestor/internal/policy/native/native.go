package native

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
)

const PreparationPurpose = "preparation-purpose"
const LocalGrantConsent = "local-grant-consent"
const SendPurposeKind = "send-purpose"
const SendPurposeDomain = "LayerX/native/send-purpose/v1\x00"
const PurposeDomain = "LayerX/native/preparation-purpose/v1\x00"
const GrantDomain = "LayerX/native/local-grant/v1\x00"

var ErrMalformed = errors.New("native consent: malformed canonical record")

type Rule struct {
	KeyID             string   `json:"key_id"`
	Owner             string   `json:"owner"`
	Tenant            string   `json:"tenant"`
	AgentDID          string   `json:"agent_did"`
	OwnerPublicKey    string   `json:"owner_public_key"`
	Operations        []string `json:"operations"`
	Capabilities      []string `json:"capabilities"`
	MaximumValidityMS uint64   `json:"maximum_validity_ms"`
	RatePerMinute     uint32   `json:"rate_per_minute"`
}
type Document struct {
	Version uint8  `json:"version"`
	Rules   []Rule `json:"rules"`
}
type Coordinates struct {
	Tenant      string
	AgentDID    string
	Session     [32]byte
	Generation  uint64
	ExpiresAtMS uint64
	Capability  [32]byte
}
type Purpose struct {
	Coordinates
	Preparation     [32]byte
	CanonicalDigest [32]byte
	Commitment      [32]byte
}

func LoadFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, ErrMalformed
	}
	if err = closedJSON(json.NewDecoder(bytes.NewReader(raw))); err != nil {
		return nil, err
	}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, ErrMalformed
	}
	if err = doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}
func closedJSON(dec *json.Decoder) error {
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				token, err = dec.Token()
				key, ok := token.(string)
				if err != nil || !ok || seen[key] {
					return ErrMalformed
				}
				seen[key] = true
				if err = walk(); err != nil {
					return err
				}
			}
			token, err = dec.Token()
			if err != nil || token != json.Delim('}') {
				return ErrMalformed
			}
		case '[':
			for dec.More() {
				if err = walk(); err != nil {
					return err
				}
			}
			token, err = dec.Token()
			if err != nil || token != json.Delim(']') {
				return ErrMalformed
			}
		default:
			return ErrMalformed
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrMalformed
	}
	return nil
}
func (d *Document) Validate() error {
	if d == nil || d.Version != 2 || len(d.Rules) == 0 {
		return ErrMalformed
	}
	seen := map[string]bool{}
	for _, r := range d.Rules {
		if r.KeyID == "" || r.Owner == "" || !validText(r.Tenant, 255) || !validText(r.AgentDID, 255) || r.MaximumValidityMS == 0 || r.RatePerMinute == 0 || len(r.Operations) == 0 || len(r.Capabilities) == 0 {
			return ErrMalformed
		}
		if _, err := Hex(r.OwnerPublicKey, 32); err != nil {
			return err
		}
		key := r.KeyID + "\x00" + r.Owner + "\x00" + r.Tenant + "\x00" + r.AgentDID
		if seen[key] {
			return ErrMalformed
		}
		seen[key] = true
		ops := map[string]bool{}
		for _, op := range r.Operations {
			if (op != PreparationPurpose && op != LocalGrantConsent && op != SendPurposeKind) || ops[op] {
				return ErrMalformed
			}
			ops[op] = true
		}
		caps := map[string]bool{}
		for _, cap := range r.Capabilities {
			if _, err := Hex(cap, 32); err != nil || caps[cap] {
				return ErrMalformed
			}
			caps[cap] = true
		}
	}
	return nil
}
func (d *Document) Evaluate(keyID, owner, operation, account string, publicKey []byte, c Coordinates, ledger policy.Ledger) policy.Decision {
	deny := func(code, reason string) policy.Decision { return policy.Decision{Code: code, Reason: reason} }
	if d == nil {
		return deny(policy.CodeDestinationDenied, "native-v2 policy is not configured")
	}
	now, err := ledger.Now()
	if err != nil || now.UnixMilli() < 0 {
		return deny(policy.CodeDestinationDenied, "native consent clock unavailable")
	}
	instant := uint64(now.UnixMilli())
	if c.ExpiresAtMS <= instant {
		return deny("native_consent_expired", "native consent has expired")
	}
	for _, r := range d.Rules {
		if r.KeyID != keyID || r.Owner != owner || r.Tenant != c.Tenant || r.AgentDID != c.AgentDID || r.OwnerPublicKey != hex.EncodeToString(publicKey) {
			continue
		}
		allowed := false
		for _, op := range r.Operations {
			allowed = allowed || op == operation
		}
		capAllowed := false
		for _, cap := range r.Capabilities {
			capAllowed = capAllowed || cap == hex.EncodeToString(c.Capability[:])
		}
		if !allowed || !capAllowed || c.ExpiresAtMS-instant > r.MaximumValidityMS {
			return deny(policy.CodeDestinationDenied, "native consent is outside explicit owner policy")
		}
		count, err := ledger.Requests(policy.AccountKey(account), now.Add(-time.Minute))
		if err != nil {
			return deny(policy.CodeDestinationDenied, "native rate ledger unavailable")
		}
		if count >= int(r.RatePerMinute) {
			return deny(policy.CodeRateLimited, "native request rate limit reached")
		}
		return policy.Decision{Allowed: true, Code: "native_owner_consent"}
	}
	return deny(policy.CodeDestinationDenied, "native owner coordinates are not configured")
}
func Hex(text string, length int) ([]byte, error) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) == 0 || hex.EncodeToString(raw) != text || (length > 0 && len(raw) != length) {
		return nil, ErrMalformed
	}
	return raw, nil
}
func validText(v string, max int) bool {
	return len(v) > 0 && len(v) <= max && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}

type reader struct {
	b   []byte
	at  int
	err error
}

func (r *reader) take(n int) []byte {
	if n < 0 || n > len(r.b)-r.at {
		r.err = ErrMalformed
		return nil
	}
	v := r.b[r.at : r.at+n]
	r.at += n
	return v
}
func (r *reader) u8() byte {
	v := r.take(1)
	if len(v) == 0 {
		return 0
	}
	return v[0]
}
func (r *reader) u16() int {
	v := r.take(2)
	if len(v) != 2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(v))
}
func (r *reader) u32() int {
	v := r.take(4)
	if len(v) != 4 {
		return 0
	}
	return int(binary.BigEndian.Uint32(v))
}
func (r *reader) u64() uint64 {
	v := r.take(8)
	if len(v) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(v)
}
func (r *reader) id() [32]byte { var v [32]byte; copy(v[:], r.take(32)); return v }
func (r *reader) text(n int) string {
	v := string(r.take(n))
	if !validText(v, 255) {
		r.err = ErrMalformed
	}
	return v
}
func (r *reader) finish() error {
	if r.err != nil || r.at != len(r.b) {
		return ErrMalformed
	}
	return nil
}
func ParsePurpose(raw []byte) (Purpose, error) {
	var p Purpose
	r := reader{b: raw}
	if string(r.take(len(PurposeDomain))) != PurposeDomain || r.u8() != 1 {
		return p, ErrMalformed
	}
	p.Tenant = r.text(r.u32())
	p.AgentDID = r.text(r.u32())
	p.Session = r.id()
	p.Generation = r.u64()
	p.ExpiresAtMS = r.u64()
	p.Capability = r.id()
	p.Preparation = r.id()
	p.CanonicalDigest = r.id()
	p.Commitment = r.id()
	if p.Generation == 0 || p.ExpiresAtMS == 0 {
		return p, ErrMalformed
	}
	return p, r.finish()
}
func PurposeDigest(raw []byte) ([32]byte, Coordinates, error) {
	p, err := ParsePurpose(raw)
	if err != nil {
		return [32]byte{}, Coordinates{}, err
	}
	return sha256.Sum256(raw), p.Coordinates, nil
}

type activity struct{ module, ordinal uint16 }

func (r *reader) activities() []activity {
	count := r.u16()
	out := make([]activity, 0, count)
	var prev activity
	for i := 0; i < count; i++ {
		version := r.u8()
		a := activity{uint16(r.u16()), uint16(r.u16())}
		if version != 1 || a.module < 1 || a.module > 11 || a.ordinal == 0 || (i > 0 && (prev.module > a.module || (prev.module == a.module && prev.ordinal >= a.ordinal))) {
			r.err = ErrMalformed
		}
		out = append(out, a)
		prev = a
	}
	return out
}

type Session struct {
	Coordinates
	activities []activity
}

func ParseSession(raw []byte) (Session, error) {
	var s Session
	r := reader{b: raw}
	if string(r.take(6)) != "LXNS01" {
		return s, ErrMalformed
	}
	s.Tenant = r.text(r.u16())
	s.AgentDID = r.text(r.u16())
	s.Session = r.id()
	s.Generation = r.u64()
	s.activities = r.activities()
	if s.Session == ([32]byte{}) || s.Generation == 0 {
		return s, ErrMalformed
	}
	return s, r.finish()
}

type Capability struct {
	Coordinates
	authority     []byte
	authorityKind byte
	expirySeconds uint64
	createdMS     uint64
	activities    []activity
}

func orderedIDs(r *reader) map[[32]byte]bool {
	count := r.u16()
	out := map[[32]byte]bool{}
	var prev [32]byte
	for i := 0; i < count; i++ {
		v := r.id()
		if i > 0 && bytes.Compare(prev[:], v[:]) >= 0 {
			r.err = ErrMalformed
		}
		out[v] = true
		prev = v
	}
	return out
}
func ParseCapability(raw []byte) (Capability, error) {
	var c Capability
	r := reader{b: raw}
	if string(r.take(4)) != "LXNC" || r.u8() != 1 {
		return c, ErrMalformed
	}
	rec := reader{b: r.take(r.u32())}
	if rec.u8() != 1 {
		return c, ErrMalformed
	}
	c.Capability = rec.id()
	switch rec.u8() {
	case 0:
	case 1:
		if rec.id() == c.Capability {
			return c, ErrMalformed
		}
	default:
		return c, ErrMalformed
	}
	c.Tenant = rec.text(rec.u16())
	c.AgentDID = rec.text(rec.u16())
	c.authorityKind = rec.u8()
	c.authority = rec.take(32)
	if c.authorityKind < 1 || c.authorityKind > 3 {
		return c, ErrMalformed
	}
	if rec.u16() != 0 {
		return c, ErrMalformed
	}
	orderedIDs(&rec)
	assets := orderedIDs(&rec)
	amounts := map[[32]byte][]byte{}
	count := rec.u16()
	var prev [32]byte
	for i := 0; i < count; i++ {
		asset := rec.id()
		amount := rec.take(16)
		if !assets[asset] || (i > 0 && bytes.Compare(prev[:], asset[:]) >= 0) {
			return c, ErrMalformed
		}
		amounts[asset] = amount
		prev = asset
	}
	count = rec.u16()
	var previousWindow uint64
	for i := 0; i < count; i++ {
		window := rec.u64()
		rec.u64()
		if window == 0 || (i > 0 && window <= previousWindow) {
			return c, ErrMalformed
		}
		previousWindow = window
	}
	if rec.u16() != 0 {
		return c, ErrMalformed
	}
	c.expirySeconds = rec.u64()
	c.ExpiresAtMS = rec.u64()
	c.createdMS = rec.u64()
	rec.u64()
	if rec.u8() != 0 || c.expirySeconds == 0 || c.ExpiresAtMS == 0 || rec.finish() != nil {
		return c, ErrMalformed
	}
	c.activities = r.activities()
	orderedIDs(&r)
	type spendKey struct {
		tag                   byte
		owner, account, asset [32]byte
		seed                  []byte
	}
	compare := func(a, b spendKey) int {
		if a.tag != b.tag {
			if a.tag < b.tag {
				return -1
			}
			return 1
		}
		if a.tag == 1 {
			if n := bytes.Compare(a.owner[:], b.owner[:]); n != 0 {
				return n
			}
			if n := bytes.Compare(a.seed, b.seed); n != 0 {
				return n
			}
			if n := bytes.Compare(a.account[:], b.account[:]); n != 0 {
				return n
			}
		}
		return bytes.Compare(a.asset[:], b.asset[:])
	}
	count = r.u16()
	var last spendKey
	for i := 0; i < count; i++ {
		key := spendKey{tag: r.u8()}
		switch key.tag {
		case 0:
		case 1:
			key.owner = r.id()
			key.seed = r.take(r.u16())
			key.account = r.id()
		default:
			return c, ErrMalformed
		}
		key.asset = r.id()
		amount := r.take(16)
		limit, ok := amounts[key.asset]
		if !ok || !assets[key.asset] || len(amount) != 16 || bytes.Compare(amount, limit) > 0 || (i > 0 && compare(last, key) >= 0) {
			return c, ErrMalformed
		}
		last = key
	}
	if err := r.finish(); err != nil {
		return c, err
	}
	return c, nil
}
func GrantDigest(capability, session []byte, expiryMS uint64, owner []byte) ([32]byte, Coordinates, error) {
	c, err := ParseCapability(capability)
	if err != nil {
		return [32]byte{}, Coordinates{}, err
	}
	s, err := ParseSession(session)
	if err != nil {
		return [32]byte{}, Coordinates{}, err
	}
	if len(owner) != 32 || c.Tenant != s.Tenant || c.AgentDID != s.AgentDID || expiryMS == 0 || expiryMS != c.ExpiresAtMS || expiryMS <= c.createdMS || c.expirySeconds > expiryMS/1000 {
		return [32]byte{}, Coordinates{}, ErrMalformed
	}
	allowed := map[activity]bool{}
	for _, a := range s.activities {
		allowed[a] = true
	}
	for _, a := range c.activities {
		if !allowed[a] {
			return [32]byte{}, Coordinates{}, ErrMalformed
		}
	}
	var out bytes.Buffer
	out.WriteString(GrantDomain)
	binary.Write(&out, binary.BigEndian, expiryMS)
	out.Write(owner)
	for _, field := range [][]byte{capability, session} {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return [32]byte{}, Coordinates{}, fmt.Errorf("%w: record length", ErrMalformed)
		}
		binary.Write(&out, binary.BigEndian, uint32(len(field)))
		out.Write(field)
	}
	coords := s.Coordinates
	coords.Capability = c.Capability
	coords.ExpiresAtMS = expiryMS
	return sha256.Sum256(out.Bytes()), coords, nil
}

type SendPurpose struct {
	Coordinates
	OwnerDID        string
	OwnerPublicKey  [32]byte
	ProtocolVersion uint16
	NetworkID       uint32
	ModuleID        uint16
	Ordinal         uint16
	Preparation     [32]byte
	CanonicalDigest [32]byte
	EconomicAction  [32]byte
	Idempotency     [32]byte
	Commitment      [32]byte
}

func ParseSendPurpose(raw []byte) (SendPurpose, error) {
	var p SendPurpose
	r := reader{b: raw}
	if string(r.take(len(SendPurposeDomain))) != SendPurposeDomain || r.u8() != 1 {
		return p, ErrMalformed
	}
	p.Tenant = r.text(r.u32())
	p.AgentDID = r.text(r.u32())
	p.OwnerDID = r.text(r.u32())
	p.OwnerPublicKey = r.id()
	p.Session = r.id()
	p.Generation = r.u64()
	p.ExpiresAtMS = r.u64()
	p.Capability = r.id()
	p.ProtocolVersion = uint16(r.u16())
	p.NetworkID = uint32(r.u32())
	version := r.u8()
	p.ModuleID = uint16(r.u16())
	p.Ordinal = uint16(r.u16())
	p.Preparation = r.id()
	p.CanonicalDigest = r.id()
	p.EconomicAction = r.id()
	p.Idempotency = r.id()
	p.Commitment = r.id()
	if p.OwnerPublicKey == ([32]byte{}) || p.OwnerDID != "did:layerx:"+hex.EncodeToString(p.OwnerPublicKey[:]) || p.Generation == 0 || p.ExpiresAtMS == 0 || p.ProtocolVersion != 3 || p.NetworkID == 0 || version != 1 || p.ModuleID != 1 || p.Ordinal != 5 {
		return p, ErrMalformed
	}
	for _, id := range [][32]byte{p.Preparation, p.CanonicalDigest, p.EconomicAction, p.Idempotency, p.Commitment} {
		if id == ([32]byte{}) {
			return p, ErrMalformed
		}
	}
	return p, r.finish()
}
func SendPurposeDigest(raw, owner []byte) ([32]byte, Coordinates, error) {
	p, err := ParseSendPurpose(raw)
	if err != nil || len(owner) != 32 || !bytes.Equal(owner, p.OwnerPublicKey[:]) {
		return [32]byte{}, Coordinates{}, ErrMalformed
	}
	return sha256.Sum256(raw), p.Coordinates, nil
}
