package accounting

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var hashPattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

func Uint(s string) (*big.Int, error) {
	if len(s) > 78 || !decimalPattern.MatchString(s) {
		return nil, ErrInvalid
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > 256 {
		return nil, ErrInvalid
	}
	return n, nil
}
func bounded(s string, bits int, positive bool) bool {
	n, e := Uint(s)
	return e == nil && n.BitLen() <= bits && (!positive || n.Sign() > 0)
}
func validAddress(s string) bool {
	return addressPattern.MatchString(s) && s != "0x"+strings.Repeat("0", 40)
}
func validDigest(s string) bool { return digestPattern.MatchString(s) }
func validLabel(s string) bool  { return labelPattern.MatchString(s) }
func ValidateSourcePosition(p SourcePosition) error {
	if p.ChainID != "125" || !bounded(p.Height, 63, false) || !bounded(p.TransactionIndex, 32, false) || !bounded(p.LogIndex, 32, false) || !hashPattern.MatchString(p.TransactionHash) || !hashPattern.MatchString(p.BlockHash) {
		return ErrInvalid
	}
	return nil
}
func CompareSourcePosition(a, b SourcePosition) (int, error) {
	if ValidateSourcePosition(a) != nil || ValidateSourcePosition(b) != nil {
		return 0, ErrInvalid
	}
	for _, pair := range [][2]string{{a.Height, b.Height}, {a.TransactionIndex, b.TransactionIndex}, {a.LogIndex, b.LogIndex}} {
		x, _ := Uint(pair[0])
		y, _ := Uint(pair[1])
		if c := x.Cmp(y); c != 0 {
			return c, nil
		}
	}
	if a.TransactionHash != b.TransactionHash || a.BlockHash != b.BlockHash {
		return 0, ErrIdentity
	}
	return 0, nil
}
func ValidateEvidence(e []EvidenceRef) error {
	if e == nil || len(e) > 64 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range e {
		if !validLabel(r.Kind) || !validLabel(r.ArtifactID) || !validDigest(r.ArtifactSHA256) || (r.LogIndex != nil && !bounded(*r.LogIndex, 32, false)) {
			return ErrInvalid
		}
		key := HashTuple("evidence-ref-v1", r.Kind, r.ArtifactSHA256, r.ArtifactID)
		if r.LogIndex != nil {
			key = HashTuple("evidence-ref-log-v1", key, *r.LogIndex)
		}
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}
func ValidateAttribution(a Attribution) error {
	if !validAddress(a.TransactionSender) || ValidateEvidence(a.Evidence) != nil {
		return ErrInvalid
	}
	actors := []*string{a.RouterCaller, a.Trader, a.Payer, a.Recipient}
	for _, p := range actors {
		if p != nil && !validAddress(*p) {
			return ErrInvalid
		}
	}
	switch a.Quality {
	case "unavailable":
		if len(a.Evidence) != 0 {
			return ErrInvalid
		}
		for _, p := range actors {
			if p != nil {
				return ErrInvalid
			}
		}
	case "exact_receipt", "qualified_calldata":
		if len(a.Evidence) == 0 {
			return ErrInvalid
		}
		found := false
		for _, p := range actors {
			found = found || p != nil
		}
		if !found {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func ValidateExecutionPolicy(e ExecutionPolicy) error {
	if ValidateEvidence(e.Evidence) != nil {
		return ErrInvalid
	}
	for _, d := range []*Digest{e.ConfigDigest, e.PreTransactionStateDigest, e.ReplayEvidenceDigest} {
		if d != nil && !validDigest(string(*d)) {
			return ErrInvalid
		}
	}
	if e.OpticalImplementation != nil && !addressPattern.MatchString(*e.OpticalImplementation) {
		return ErrInvalid
	}
	if e.FeeBPS != nil && *e.FeeBPS > 10000 {
		return ErrInvalid
	}
	switch e.Quality {
	case "unavailable":
		if e.FeeBPS != nil || e.ConfigDigest != nil || e.PreTransactionStateDigest != nil || e.ReplayEvidenceDigest != nil || e.OpticalImplementation != nil || len(e.Evidence) != 0 {
			return ErrInvalid
		}
	case "exact", "qualified_execution":
		if len(e.Evidence) == 0 || e.ConfigDigest == nil || e.PreTransactionStateDigest == nil || e.ReplayEvidenceDigest == nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func ValidateFillV1(f FillV1) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalid, why) }
	if f.Version != FillVersion || ValidateSourcePosition(f.Position) != nil {
		return bad("version or position")
	}
	if !validAddress(f.ActualEmitter) || !validAddress(f.PoolAddress) || !validAddress(f.Token.Address) || !validAddress(f.Settlement.Address) || f.Token.Address == f.Settlement.Address || f.Settlement.Decimals != 6 {
		return bad("assets or emitter")
	}
	p := f.Provenance
	if !validLabel(p.DeploymentID) || !validLabel(p.ABIRevision) || !validLabel(p.EmitterCodeEpoch) || !validLabel(p.PoolCodeEpoch) || !validDigest(p.ReceiptSHA256) || !validDigest(p.HeaderSHA256) {
		return bad("provenance")
	}
	if f.SourceFillID != SourceFillIdentity(f.Position, p.DeploymentID) {
		return ErrIdentity
	}
	if ValidateAttribution(f.Attribution) != nil || ValidateExecutionPolicy(f.Execution) != nil {
		return bad("actor or execution evidence")
	}
	if !bounded(f.BlockTimestampSeconds, 63, false) {
		return bad("timestamp")
	}
	ts, _ := Uint(f.BlockTimestampSeconds)
	if ts.Cmp(new(big.Int).Quo(new(big.Int).SetUint64(9223372036854775807), big.NewInt(1000))) > 0 {
		return bad("timestamp milliseconds overflow")
	}
	token, e := Uint(f.TokenAmountRaw)
	if e != nil || token.Sign() == 0 {
		return bad("token amount")
	}
	usdl, e := Uint(f.USDLAmountRaw)
	if e != nil || usdl.Sign() == 0 {
		return bad("settlement amount")
	}
	paid, e := Uint(f.PaidInputRaw)
	if e != nil || paid.Sign() == 0 {
		return bad("paid input")
	}
	fee, e := Uint(f.FeeRaw)
	if e != nil || fee.Cmp(paid) > 0 {
		return bad("fee")
	}
	switch f.Side {
	case "buy":
		if f.PaidInputRaw != f.USDLAmountRaw || f.FeeDisposition != "distributed_usdl" {
			return bad("buy units")
		}
	case "sell":
		if f.PaidInputRaw != f.TokenAmountRaw || f.FeeDisposition != "retained_token_reserve" {
			return bad("sell units")
		}
	default:
		return bad("side")
	}
	if f.PricedInputRaw == nil {
		if f.AMMInputAfterFeeRaw != nil {
			return bad("net without priced input")
		}
		switch f.InputDerivationQuality {
		case "receipt_incomplete", "abi_unqualified", "legacy_unavailable":
		default:
			return bad("unavailable input quality")
		}
	} else {
		if f.InputDerivationQuality != "exact" || f.AMMInputAfterFeeRaw == nil {
			return bad("priced input quality")
		}
		priced, e := Uint(*f.PricedInputRaw)
		if e != nil {
			return bad("priced input")
		}
		net, e := Uint(*f.AMMInputAfterFeeRaw)
		if e != nil {
			return bad("net input")
		}
		expected := new(big.Int).Sub(priced, fee)
		if expected.Sign() < 0 || expected.Cmp(net) != 0 {
			return bad("net input arithmetic")
		}
		if f.Execution.Quality == "unavailable" {
			return bad("priced input authority")
		}
		if f.Execution.FeeBPS != nil {
			expected.Mul(priced, new(big.Int).SetUint64(uint64(*f.Execution.FeeBPS)))
			expected.Quo(expected, big.NewInt(10000))
			if expected.Cmp(fee) != 0 {
				return bad("fee rate arithmetic")
			}
		}
	}
	return nil
}
func ValidatePartitionPosition(p PartitionPosition) error {
	if !validLabel(p.ProjectionVersion) || len(p.ProjectionVersion) > 128 || !bounded(p.Sequence, 63, true) || !validDigest(p.SourceEventID) || !validDigest(p.EffectIdentity) || !validDigest(string(p.PayloadDigest)) || ValidateSourcePosition(p.SourceOrder) != nil {
		return ErrInvalid
	}
	if p.Sequence == "1" {
		if p.PreviousEffectIdentity != "" {
			return ErrInvalid
		}
	} else if !validDigest(p.PreviousEffectIdentity) || p.PreviousEffectIdentity == p.EffectIdentity {
		return ErrInvalid
	}
	parts := strings.Split(p.PartitionKey, ":")
	switch p.PartitionKind {
	case "market":
		if len(parts) != 2 || parts[0] != "125" || !validAddress(parts[1]) {
			return ErrInvalid
		}
	case "inventory":
		if len(parts) != 3 || parts[0] != "125" || !validAddress(parts[1]) || !validAddress(parts[2]) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func ValidateEnrichmentV1(e EnrichmentV1) error {
	if !validDigest(e.SourceFillID) || !validDigest(string(e.SourceFactDigest)) || !validDigest(string(e.EvidenceDigest)) || !validLabel(e.EnrichmentVersion) || e.EnrichmentVersion == "initial" || !validLabel(e.ProjectionVersion) || len(e.ProjectionVersion) > 128 || (e.Facet != "market" && e.Facet != "inventory") || (e.Attribution == nil && e.Execution == nil) {
		return ErrInvalid
	}
	if e.Attribution != nil && ValidateAttribution(*e.Attribution) != nil {
		return ErrInvalid
	}
	if e.Execution != nil && ValidateExecutionPolicy(*e.Execution) != nil {
		return ErrInvalid
	}
	if e.Facet == "inventory" && (e.Attribution == nil || e.Attribution.Payer == nil || e.Attribution.Recipient == nil || *e.Attribution.Payer != *e.Attribution.Recipient || e.Attribution.Quality == "unavailable") {
		return ErrInvalid
	}
	return nil
}
func ValidatePublicationV1(p PublicationV1) error {
	if p.Version != PublicationVersion || !validDigest(p.PublicationID) || !validDigest(p.SourceFillID) || !validDigest(string(p.SourceFactDigest)) || (p.Facet != "market" && p.Facet != "inventory") || (p.Fill == nil) == (p.Enrichment == nil) || len(p.Positions) != 1 {
		return ErrInvalid
	}
	position := p.Positions[0]
	if ValidatePartitionPosition(position) != nil || position.SourceEventID != p.SourceFillID || position.PartitionKind != p.Facet {
		return ErrIdentity
	}
	evidence := Digest("")
	enrichmentVersion := "initial"
	if p.Fill != nil {
		if p.Facet != "market" || ValidateFillV1(*p.Fill) != nil || p.Fill.SourceFillID != p.SourceFillID || position.SourceOrder != p.Fill.Position || position.PartitionKey != "125:"+p.Fill.PoolAddress {
			return ErrIdentity
		}
		d, err := FillDigest(*p.Fill)
		if err != nil || d != p.SourceFactDigest {
			return ErrIdentity
		}
	} else {
		e := p.Enrichment
		if ValidateEnrichmentV1(*e) != nil || e.SourceFillID != p.SourceFillID || e.SourceFactDigest != p.SourceFactDigest || e.Facet != p.Facet || e.ProjectionVersion != position.ProjectionVersion {
			return ErrIdentity
		}
		evidence = e.EvidenceDigest
		enrichmentVersion = e.EnrichmentVersion
		if e.Facet == "inventory" && strings.Split(position.PartitionKey, ":")[1] != *e.Attribution.Payer {
			return ErrIdentity
		}
	}
	d, err := FacetDigest(p)
	if err != nil || d != position.PayloadDigest {
		return ErrIdentity
	}
	if position.EffectIdentity != EffectIdentity(p.SourceFillID, p.Facet, position.ProjectionVersion, evidence, position.PartitionKind, position.PartitionKey) || p.PublicationID != PublicationIdentity(p.SourceFillID, p.SourceFactDigest, p.Facet, position.ProjectionVersion, enrichmentVersion, evidence) {
		return ErrIdentity
	}
	return nil
}
