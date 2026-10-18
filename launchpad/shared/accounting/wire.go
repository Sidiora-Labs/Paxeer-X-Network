package accounting

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxWireBytes = 1 << 20
const FillVersion = "accounting-fill-v1"
const PublicationVersion = "accounting-publication-v1"
const MarketSubject = "accounting.market.v1"
const InventorySubject = "accounting.inventory.v1"

var ErrInvalid = errors.New("invalid accounting value")
var ErrInvalidWire = errors.New("invalid accounting wire")
var ErrIdentity = errors.New("accounting identity mismatch")

type Digest string

type SourcePosition struct {
	ChainID          string `json:"chainId"`
	Height           string `json:"height"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex         string `json:"logIndex"`
	TransactionHash  string `json:"transactionHash"`
	BlockHash        string `json:"blockHash"`
}
type EvidenceRef struct {
	Kind           string  `json:"kind"`
	ArtifactSHA256 string  `json:"artifactSHA256"`
	ArtifactID     string  `json:"artifactId"`
	LogIndex       *string `json:"logIndex"`
}
type Asset struct {
	Address  string `json:"address"`
	Decimals uint8  `json:"decimals"`
}
type Provenance struct {
	DeploymentID     string `json:"deploymentId"`
	ABIRevision      string `json:"abiRevision"`
	EmitterCodeEpoch string `json:"emitterCodeEpoch"`
	PoolCodeEpoch    string `json:"poolCodeEpoch"`
	ReceiptSHA256    string `json:"receiptSHA256"`
	HeaderSHA256     string `json:"headerSHA256"`
}
type Attribution struct {
	TransactionSender string        `json:"transactionSender"`
	RouterCaller      *string       `json:"routerCaller"`
	Trader            *string       `json:"trader"`
	Payer             *string       `json:"payer"`
	Recipient         *string       `json:"recipient"`
	Quality           string        `json:"quality"`
	Evidence          []EvidenceRef `json:"evidence"`
}
type ExecutionPolicy struct {
	Quality                   string        `json:"quality"`
	FeeBPS                    *uint16       `json:"feeBps"`
	ConfigDigest              *Digest       `json:"configDigest"`
	PreTransactionStateDigest *Digest       `json:"preTransactionStateDigest"`
	ReplayEvidenceDigest      *Digest       `json:"replayEvidenceDigest"`
	OpticalImplementation     *string       `json:"opticalImplementation"`
	Evidence                  []EvidenceRef `json:"evidence"`
}
type FillV1 struct {
	Version                string          `json:"version"`
	SourceFillID           string          `json:"sourceFillId"`
	Position               SourcePosition  `json:"position"`
	ActualEmitter          string          `json:"actualEmitter"`
	PoolAddress            string          `json:"poolAddress"`
	Token                  Asset           `json:"token"`
	Settlement             Asset           `json:"settlement"`
	Side                   string          `json:"side"`
	TokenAmountRaw         string          `json:"tokenAmountRaw"`
	USDLAmountRaw          string          `json:"usdlAmountRaw"`
	PaidInputRaw           string          `json:"paidInputRaw"`
	FeeRaw                 string          `json:"feeRaw"`
	PricedInputRaw         *string         `json:"pricedInputRaw"`
	AMMInputAfterFeeRaw    *string         `json:"ammInputAfterFeeRaw"`
	InputDerivationQuality string          `json:"inputDerivationQuality"`
	FeeDisposition         string          `json:"feeDisposition"`
	Attribution            Attribution     `json:"attribution"`
	Execution              ExecutionPolicy `json:"execution"`
	Provenance             Provenance      `json:"provenance"`
	BlockTimestampSeconds  string          `json:"blockTimestampSeconds"`
}
type PartitionPosition struct {
	ProjectionVersion      string         `json:"projectionVersion"`
	PartitionKind          string         `json:"partitionKind"`
	PartitionKey           string         `json:"partitionKey"`
	Sequence               string         `json:"sequence"`
	PreviousEffectIdentity string         `json:"previousEffectIdentity"`
	SourceEventID          string         `json:"sourceEventId"`
	EffectIdentity         string         `json:"effectIdentity"`
	PayloadDigest          Digest         `json:"payloadDigest"`
	SourceOrder            SourcePosition `json:"sourceOrder"`
}
type PublicationV1 struct {
	Version          string              `json:"version"`
	PublicationID    string              `json:"publicationId"`
	Facet            string              `json:"facet"`
	SourceFillID     string              `json:"sourceFillId"`
	SourceFactDigest Digest              `json:"sourceFactDigest"`
	Fill             *FillV1             `json:"fill"`
	Enrichment       *EnrichmentV1       `json:"enrichment"`
	Positions        []PartitionPosition `json:"positions"`
}
type EnrichmentV1 struct {
	SourceFillID      string           `json:"sourceFillId"`
	SourceFactDigest  Digest           `json:"sourceFactDigest"`
	Facet             string           `json:"facet"`
	EnrichmentVersion string           `json:"enrichmentVersion"`
	ProjectionVersion string           `json:"projectionVersion"`
	EvidenceDigest    Digest           `json:"evidenceDigest"`
	Attribution       *Attribution     `json:"attribution"`
	Execution         *ExecutionPolicy `json:"execution"`
}

func HashTuple(domain string, components ...string) string {
	h := sha256.New()
	for _, s := range append([]string{domain}, components...) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(s)))
		h.Write(size[:])
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func SourceFillIdentity(p SourcePosition, deploymentID string) string {
	return HashTuple("source-fill-v1", p.ChainID, deploymentID, p.TransactionHash, p.LogIndex)
}
func PublicationIdentity(sourceFillID string, sourceFactDigest Digest, facet, projectionVersion, enrichmentVersion string, evidenceDigest Digest) string {
	return HashTuple("publication-v1", sourceFillID, string(sourceFactDigest), facet, projectionVersion, enrichmentVersion, string(evidenceDigest))
}
func EffectIdentity(sourceFillID, facet, projectionVersion string, evidenceDigest Digest, partitionKind, partitionKey string) string {
	return HashTuple("effect-v1", sourceFillID, facet, projectionVersion, string(evidenceDigest), partitionKind, partitionKey)
}
func digestBody(v any) (Digest, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return Digest(hex.EncodeToString(h[:])), nil
}
func FillDigest(f FillV1) (Digest, error) {
	if err := ValidateFillV1(f); err != nil {
		return "", err
	}
	return digestBody(f)
}
func FacetDigest(p PublicationV1) (Digest, error) {
	if (p.Fill == nil) == (p.Enrichment == nil) {
		return "", ErrInvalid
	}
	if p.Fill != nil {
		return digestBody(*p.Fill)
	}
	return digestBody(*p.Enrichment)
}
func CanonicalPublicationV1(p PublicationV1) ([]byte, error) {
	if err := ValidatePublicationV1(p); err != nil {
		return nil, err
	}
	p.Positions = append([]PartitionPosition(nil), p.Positions...)
	sort.Slice(p.Positions, func(i, j int) bool {
		a, b := p.Positions[i], p.Positions[j]
		if a.ProjectionVersion != b.ProjectionVersion {
			return a.ProjectionVersion < b.ProjectionVersion
		}
		if a.PartitionKind != b.PartitionKind {
			return a.PartitionKind < b.PartitionKind
		}
		return a.PartitionKey < b.PartitionKey
	})
	b, err := json.Marshal(p)
	if err == nil && len(b) > MaxWireBytes {
		return nil, ErrInvalidWire
	}
	return b, err
}
func NewMarketPublication(f FillV1, positions []PartitionPosition) (PublicationV1, error) {
	d, err := FillDigest(f)
	if err != nil {
		return PublicationV1{}, err
	}
	p := PublicationV1{Version: PublicationVersion, Facet: "market", SourceFillID: f.SourceFillID, SourceFactDigest: d, Fill: &f, Positions: append([]PartitionPosition(nil), positions...)}
	if len(positions) != 1 {
		return PublicationV1{}, ErrInvalid
	}
	p.PublicationID = PublicationIdentity(f.SourceFillID, d, "market", positions[0].ProjectionVersion, "initial", "")
	return p, ValidatePublicationV1(p)
}
func NewEnrichmentPublication(e EnrichmentV1, positions []PartitionPosition) (PublicationV1, error) {
	p := PublicationV1{Version: PublicationVersion, Facet: e.Facet, SourceFillID: e.SourceFillID, SourceFactDigest: e.SourceFactDigest, Enrichment: &e, Positions: append([]PartitionPosition(nil), positions...)}
	p.PublicationID = PublicationIdentity(e.SourceFillID, e.SourceFactDigest, e.Facet, e.ProjectionVersion, e.EnrichmentVersion, e.EvidenceDigest)
	return p, ValidatePublicationV1(p)
}
func SubjectForFacet(facet string) (string, error) {
	switch facet {
	case "market":
		return MarketSubject, nil
	case "inventory":
		return InventorySubject, nil
	default:
		return "", ErrInvalid
	}
}
func DecodePublicationV1(raw []byte) (PublicationV1, error) {
	var p PublicationV1
	if err := decodeStrict(raw, &p); err != nil {
		return p, err
	}
	return p, ValidatePublicationV1(p)
}
func DecodeFillV1(raw []byte) (FillV1, error) {
	var f FillV1
	if err := decodeStrict(raw, &f); err != nil {
		return f, err
	}
	return f, ValidateFillV1(f)
}
func decodeStrict(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > MaxWireBytes || !utf8.Valid(raw) {
		return ErrInvalidWire
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := scanJSON(d, 0); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWire, err)
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalidWire
	}
	var shape any
	d = json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&shape); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWire, err)
	}
	if err := validateShape(shape, reflect.TypeOf(target).Elem()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWire, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWire, err)
	}
	return nil
}
func scanJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("nesting limit")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate object key")
			}
			seen[s] = true
			if err := scanJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := scanJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	end, err := d.Token()
	if err != nil {
		return err
	}
	if (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errors.New("unclosed JSON")
	}
	return nil
}
func validateShape(v any, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		return validateShape(v, t.Elem())
	}
	if v == nil {
		return errors.New("nonnullable field")
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return errors.New("object required")
		}
		if len(obj) != t.NumField() {
			return errors.New("unknown or missing field")
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			child, exists := obj[name]
			if !exists {
				return fmt.Errorf("missing field %s", name)
			}
			if err := validateShape(child, f.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return errors.New("array required")
		}
		for _, child := range a {
			if err := validateShape(child, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return errors.New("string required")
		}
	case reflect.Uint8, reflect.Uint16:
		n, ok := v.(json.Number)
		if !ok || strings.ContainsAny(string(n), "-.eE+") {
			return errors.New("unsigned integer required")
		}
	default:
		return errors.New("unsupported wire field")
	}
	return nil
}
