package l17

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ShareSnapshot is an allowlist, never a serialization of a private account.
// Its values must come from a committed server-side summary, not request amounts.
type ShareSnapshot struct {
	RangeStart                   *time.Time        `json:"rangeStart,omitempty"`
	RangeEnd                     *time.Time        `json:"rangeEnd,omitempty"`
	QuoteIdentity                *AssetRef         `json:"quoteIdentity,omitempty"`
	HistoricalReturn             *HistoricalReturn `json:"historicalReturn,omitempty"`
	Version                      int               `json:"version"`
	SnapshotID                   string            `json:"snapshotId"`
	Wallet                       *string           `json:"wallet,omitempty"`
	QuoteAsset                   string            `json:"quoteAsset"`
	Decimals                     int               `json:"decimals"`
	Methodology                  string            `json:"methodology"`
	Watermark                    Watermark         `json:"watermark"`
	KnownRealizedSubtotalUsdlRaw string            `json:"knownRealizedSubtotalUsdlRaw"`
	RealizedPnlRaw               *string           `json:"realizedPnlRaw"`
	CompleteValueUsdlRaw         *string           `json:"completeValueUsdlRaw"`
	BasisQuality                 string            `json:"basisQuality"`
	PricingQuality               string            `json:"pricingQuality"`
	Label                        string            `json:"label"`
}

func (r Repository) Share(ctx context.Context, wallet, verifiedWallet, snapshot string, includeWallet bool, now time.Time) (ShareSnapshot, error) {
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return ShareSnapshot{}, e
	}
	page, e := r.Page(ctx, wallet, snapshot, -1, 1, now)
	if e != nil {
		return ShareSnapshot{}, e
	}
	s := page.Summary
	out := ShareSnapshot{Version: 1, SnapshotID: s.SnapshotID, QuoteAsset: "USDL", Decimals: 6, Methodology: Algorithm, Watermark: s.Watermark, KnownRealizedSubtotalUsdlRaw: s.KnownRealizedSubtotalUsdlRaw, RealizedPnlRaw: s.RealizedPnlRaw, CompleteValueUsdlRaw: s.CompleteValueUsdlRaw, BasisQuality: s.BasisQuality, PricingQuality: s.PricingQuality, Label: "Known realized result (USDL); total profit unavailable"}
	if includeWallet {
		out.Wallet = ptr(s.Wallet)
	}
	return out, nil
}

// SafeMetadata is suitable for HTML text only. No remote image fetch is provided.
func SafeMetadata(s string) (string, error) {
	if len(s) > 256 {
		return "", ErrInvalid
	}
	return html.EscapeString(s), nil
}

type DisplayQuote struct {
	Numerator   string    `json:"numerator"`
	Denominator string    `json:"denominator"`
	Provider    string    `json:"provider"`
	ObservedAt  time.Time `json:"observedAt"`
	Stale       bool      `json:"stale"`
}
type USDDisplay struct {
	UsdlRaw  string        `json:"usdlRaw"`
	UsdMicro *string       `json:"usdMicro"`
	Quote    *DisplayQuote `json:"quote"`
}

func ConvertDisplay(raw string, q *DisplayQuote) (USDDisplay, error) {
	out := USDDisplay{UsdlRaw: raw, Quote: q}
	v, e := number(raw, false, 156)
	if e != nil {
		return out, e
	}
	if q == nil {
		return out, nil
	}
	n, e := number(q.Numerator, false, 512)
	if e != nil {
		return out, e
	}
	d, e := number(q.Denominator, false, 512)
	if e != nil || d.Sign() == 0 || n.Sign() == 0 || q.Provider == "" || len(q.Provider) > 256 || q.ObservedAt.IsZero() {
		return out, ErrInvalid
	}
	if q.Stale {
		return out, nil
	}
	v.Mul(v, n).Quo(v, d)
	if len(v.String()) > 156 {
		return out, ErrInvalid
	}
	out.UsdMicro = ptr(v.String())
	return out, nil
}

type ExportManifest struct {
	Object     string    `json:"object"`
	Rows       int       `json:"rows"`
	SHA256     string    `json:"sha256"`
	SnapshotID string    `json:"snapshotId"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Format     string    `json:"format"`
}

// ExportJSON writes JSON Lines in bounded snapshot pages and atomically renames
// only the complete file. The private directory must be service-owned. No URL or
// download endpoint is exposed here; owner auth is required again at download.
func (r Repository) ExportJSON(ctx context.Context, dir, wallet, verifiedWallet, snapshot string, now time.Time) (manifest ExportManifest, err error) {
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return manifest, e
	}
	id, e := opaqueID()
	if e != nil {
		return manifest, e
	}
	if e = privateExportDirectory(dir); e != nil {
		return manifest, e
	}
	f, e := os.OpenFile(filepath.Join(dir, id+".partial"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return manifest, e
	}
	partial := f.Name()
	complete := filepath.Join(dir, id+".jsonl")
	success := false
	defer func() {
		f.Close()
		os.Remove(partial)
		if !success {
			os.Remove(complete)
		}
	}()
	hash := sha256.New()
	writer := io.MultiWriter(f, hash)
	enc := json.NewEncoder(writer)
	after := -1
	count := 0
	for {
		if e = ctx.Err(); e != nil {
			return manifest, e
		}
		page, e := r.Page(ctx, wallet, snapshot, after, 100, now)
		if e != nil {
			return manifest, e
		}
		for _, p := range page.Positions {
			if e = enc.Encode(p); e != nil {
				return manifest, e
			}
			count++
		}
		after += len(page.Positions)
		if count == page.Summary.PositionCount {
			break
		}
		if len(page.Positions) == 0 {
			return manifest, errors.New("snapshot row count mismatch")
		}
	}
	if e = f.Sync(); e != nil {
		return manifest, e
	}
	if e = f.Close(); e != nil {
		return manifest, e
	}
	if e = ctx.Err(); e != nil {
		return manifest, e
	}
	if e = os.Rename(partial, complete); e != nil {
		return manifest, e
	}
	if e = syncExportDirectory(dir); e != nil {
		return manifest, e
	}
	success = true
	return ExportManifest{Object: id + ".jsonl", Rows: count, SHA256: hex.EncodeToString(hash.Sum(nil)), SnapshotID: snapshot, ExpiresAt: now.Add(15 * time.Minute), Format: "jsonl"}, nil
}

const MetadataDDL = `CREATE TABLE portfolio_snapshot_history_metadata (snapshot_id uuid PRIMARY KEY REFERENCES portfolio_snapshots(id),source_event text NOT NULL UNIQUE,payload jsonb NOT NULL);`

type SnapshotHistoryMetadata struct {
	SourceEvent string         `json:"sourceEvent"`
	Start       time.Time      `json:"start"`
	End         time.Time      `json:"end"`
	Quote       AssetRef       `json:"quote"`
	Periods     []ReturnPeriod `json:"periods"`
}

func (r Repository) CommitHistoryMetadataTx(ctx context.Context, tx pgx.Tx, wallet, snapshot string, m SnapshotHistoryMetadata) error {
	if tx == nil || !validUUID(snapshot) || len(m.SourceEvent) == 0 || len(m.SourceEvent) > 256 || m.Start.IsZero() || !m.Start.Before(m.End) {
		return ErrInvalid
	}
	if m.Quote.Kind != "erc20" || m.Quote.ChainID == nil || *m.Quote.ChainID != "125" || m.Quote.Address == nil || m.Quote.Decimals == nil || *m.Quote.Decimals != 6 || m.Quote.CurrencyCode == nil || *m.Quote.CurrencyCode != "USDL" {
		return ErrInvalid
	}
	if _, e := Wallet(*m.Quote.Address); e != nil {
		return e
	}
	if !strings.EqualFold(*m.Quote.Address, "0x85FcD13735F4309833A503EE804ea32395851479") {
		return ErrInvalid
	}
	if len(m.Periods) > 0 {
		if !m.Periods[0].Start.Equal(m.Start) || !m.Periods[len(m.Periods)-1].End.Equal(m.End) {
			return ErrInvalid
		}
		if _, e := TimeWeightedReturn(m.Periods); e != nil {
			return e
		}
	}
	raw, e := json.Marshal(m)
	if e != nil {
		return e
	}
	var owner string
	if e = tx.QueryRow(ctx, "SELECT wallet FROM portfolio_snapshots WHERE id=$1", snapshot).Scan(&owner); e != nil {
		return e
	}
	if e = RequireOwner(wallet, owner); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "INSERT INTO portfolio_snapshot_history_metadata VALUES($1,$2,$3) ON CONFLICT(snapshot_id) DO UPDATE SET payload=portfolio_snapshot_history_metadata.payload WHERE portfolio_snapshot_history_metadata.payload=$3::jsonb AND portfolio_snapshot_history_metadata.source_event=$2", snapshot, m.SourceEvent, raw)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (r Repository) ShareV2(ctx context.Context, wallet, verified, snapshot string, includeWallet bool, now time.Time) (ShareSnapshot, error) {
	out, e := r.Share(ctx, wallet, verified, snapshot, includeWallet, now)
	if e != nil {
		return out, e
	}
	var raw []byte
	e = r.Pool.QueryRow(ctx, "SELECT m.payload FROM portfolio_snapshot_history_metadata m JOIN portfolio_snapshots s ON s.id=m.snapshot_id WHERE m.snapshot_id=$1 AND s.wallet=$2", snapshot, outWatermarkWallet(wallet)).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrUnavailable
	}
	if e != nil {
		return out, e
	}
	var m SnapshotHistoryMetadata
	if e = json.Unmarshal(raw, &m); e != nil {
		return out, e
	}
	out.Version = 2
	out.RangeStart = &m.Start
	out.RangeEnd = &m.End
	out.QuoteIdentity = &m.Quote
	historical := HistoricalReturn{Method: "unavailable", Reason: "incomplete_cash_flow_or_valuation_history"}
	if len(m.Periods) > 0 {
		historical, e = TimeWeightedReturn(m.Periods)
		if e != nil {
			return out, e
		}
	}
	out.HistoricalReturn = &historical
	return out, nil
}
func outWatermarkWallet(wallet string) string { normalized, _ := Wallet(wallet); return normalized }
