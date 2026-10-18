package l17

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
)

const HistoryDDL = `CREATE TABLE portfolio_history_snapshots (
 id uuid PRIMARY KEY REFERENCES portfolio_snapshots(id), source_event text NOT NULL UNIQUE,
 digest text NOT NULL, row_count integer NOT NULL CHECK(row_count>=0));
 CREATE TABLE portfolio_history_rows (
 snapshot_id uuid NOT NULL REFERENCES portfolio_history_snapshots(id), ordinal integer NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY(snapshot_id,ordinal));
 CREATE TABLE portfolio_history_outbox (
 event_id uuid PRIMARY KEY REFERENCES portfolio_history_snapshots(id), payload jsonb NOT NULL,
 published_at timestamptz);`

var historyEventID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,255}$`)

type HistoryRecord struct {
	EventID            string        `json:"eventId"`
	Kind               string        `json:"kind"`
	Asset              AssetRef      `json:"asset"`
	AmountRaw          string        `json:"amountRaw"`
	QuoteAmountUsdlRaw *string       `json:"quoteAmountUsdlRaw"`
	BasisQuality       string        `json:"basisQuality"`
	Position           ChainPosition `json:"position"`
}

func validateHistory(h HistoryRecord) error {
	if !historyEventID.MatchString(h.EventID) {
		return ErrInvalid
	}
	switch h.Kind {
	case "trade", "deposit", "withdrawal", "transfer_in", "transfer_out":
	default:
		return ErrInvalid
	}
	if _, e := uint256(h.AmountRaw); e != nil {
		return e
	}
	if h.QuoteAmountUsdlRaw != nil {
		if _, e := number(*h.QuoteAmountUsdlRaw, false, 156); e != nil {
			return e
		}
	}
	switch h.BasisQuality {
	case "complete", "partial", "unknown":
	default:
		return ErrInvalid
	}
	if h.Asset.Kind != "erc20" || h.Asset.ChainID == nil || *h.Asset.ChainID != "125" || h.Asset.Address == nil || h.Asset.Decimals == nil || *h.Asset.Decimals < 0 || *h.Asset.Decimals > 255 {
		return ErrInvalid
	}
	if _, e := Wallet(*h.Asset.Address); e != nil {
		return e
	}
	p := h.Position
	if p.ChainID != "125" || len(p.BlockHash) != 66 || len(p.TransactionHash) != 66 {
		return ErrInvalid
	}
	for _, v := range []string{p.Height, p.TransactionIndex, p.LogIndex} {
		if _, e := uint256(v); e != nil {
			return e
		}
	}
	for _, v := range []string{p.BlockHash, p.TransactionHash} {
		if _, e := hex.DecodeString(v[2:]); e != nil || v[:2] != "0x" {
			return ErrInvalid
		}
	}
	return nil
}

func (r Repository) CommitHistory(ctx context.Context, wallet, snapshot, sourceEvent string, records []HistoryRecord) error {
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	if e = r.CommitHistoryTx(ctx, tx, wallet, snapshot, sourceEvent, records); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r Repository) CommitHistoryTx(ctx context.Context, tx pgx.Tx, wallet, snapshot, sourceEvent string, records []HistoryRecord) error {
	if tx == nil {
		return ErrInvalid
	}
	wallet, e := Wallet(wallet)
	if e != nil {
		return e
	}
	if !validUUID(snapshot) || sourceEvent == "" || len(sourceEvent) > 256 || len(records) > 1000000 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	blocks := map[string]string{}
	transactions := map[string]string{}
	var previous *HistoryRecord
	for i := range records {
		h := records[i]
		if e := validateHistory(h); e != nil {
			return e
		}
		block := strings.ToLower(h.Position.BlockHash)
		transaction := strings.ToLower(h.Position.TransactionHash)
		transactionKey := h.Position.Height + ":" + h.Position.TransactionIndex
		if prior, exists := blocks[h.Position.Height]; exists && prior != block {
			return ErrConflict
		}
		if prior, exists := transactions[transactionKey]; exists && prior != transaction {
			return ErrConflict
		}
		blocks[h.Position.Height] = block
		transactions[transactionKey] = transaction
		if seen[h.EventID] {
			return ErrInvalid
		}
		seen[h.EventID] = true
		if previous != nil {
			order := 0
			for j, a := range []string{previous.Position.Height, previous.Position.TransactionIndex, previous.Position.LogIndex} {
				b := []string{h.Position.Height, h.Position.TransactionIndex, h.Position.LogIndex}[j]
				an, _ := new(big.Int).SetString(a, 10)
				bn, _ := new(big.Int).SetString(b, 10)
				order = an.Cmp(bn)
				if order != 0 {
					break
				}
			}
			if order >= 0 {
				return ErrInvalid
			}
		}
		previous = &records[i]
	}
	raw, e := json.Marshal(records)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", sourceEvent); e != nil {
		return e
	}
	var previousDigest, previousSnapshot, previousWallet string
	e = tx.QueryRow(ctx, "SELECT h.digest,h.id::text,s.wallet FROM portfolio_history_snapshots h JOIN portfolio_snapshots s ON s.id=h.id WHERE h.source_event=$1", sourceEvent).Scan(&previousDigest, &previousSnapshot, &previousWallet)
	if e == nil {
		if wallet != previousWallet {
			return ErrUnauthorized
		}
		if digest != previousDigest || snapshot != previousSnapshot {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var snapshotRaw []byte
	if e = tx.QueryRow(ctx, "SELECT summary FROM portfolio_snapshots WHERE id=$1 AND wallet=$2", snapshot, wallet).Scan(&snapshotRaw); errors.Is(e, pgx.ErrNoRows) {
		return ErrUnauthorized
	} else if e != nil {
		return e
	}
	var summary Summary
	if e = json.Unmarshal(snapshotRaw, &summary); e != nil {
		return e
	}
	height, _ := uint256(summary.Watermark.Height)
	for _, h := range records {
		eventHeight, _ := uint256(h.Position.Height)
		if eventHeight.Cmp(height) > 0 || (eventHeight.Cmp(height) == 0 && !strings.EqualFold(h.Position.BlockHash, summary.Watermark.BlockHash)) {
			return ErrInvalid
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_history_snapshots VALUES($1,$2,$3,$4)", snapshot, sourceEvent, digest, len(records)); e != nil {
		return e
	}
	values := make([][]any, len(records))
	for i, h := range records {
		data, e := json.Marshal(h)
		if e != nil {
			return e
		}
		values[i] = []any{snapshot, i, data}
	}
	if _, e = tx.CopyFrom(ctx, pgx.Identifier{"portfolio_history_rows"}, []string{"snapshot_id", "ordinal", "payload"}, pgx.CopyFromRows(values)); e != nil {
		return e
	}
	notice, _ := json.Marshal(struct {
		SnapshotID string `json:"snapshotId"`
		Wallet     string `json:"wallet"`
		Rows       int    `json:"rows"`
	}{snapshot, wallet, len(records)})
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_history_outbox(event_id,payload) VALUES($1,$2)", snapshot, notice); e != nil {
		return e
	}
	return nil
}

type HistoryPage struct {
	Records []HistoryRecord `json:"records"`
	Rows    int             `json:"rows"`
}

func (r Repository) HistoryPage(ctx context.Context, wallet, verifiedWallet, snapshot string, after, limit int, now time.Time) (HistoryPage, error) {
	var out HistoryPage
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return out, e
	}
	wallet, _ = Wallet(wallet)
	limit, e := Limit(limit)
	if e != nil || after < -1 || after > 1000000 || !validUUID(snapshot) {
		return out, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	admission := r.ReadAdmission
	if admission == nil {
		admission = defaultAdmission
	}
	release, e := admission.Acquire(ctx, wallet)
	if e != nil {
		return out, e
	}
	defer release()
	var expires time.Time
	e = r.Pool.QueryRow(ctx, `SELECT h.row_count,s.expires_at FROM portfolio_history_snapshots h JOIN portfolio_snapshots s ON s.id=h.id WHERE s.id=$1 AND s.wallet=$2`, snapshot, wallet).Scan(&out.Rows, &expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrRestart
	}
	if e != nil {
		return out, e
	}
	if !now.Before(expires) {
		return out, ErrRestart
	}
	rows, e := r.Pool.Query(ctx, "SELECT payload FROM portfolio_history_rows WHERE snapshot_id=$1 AND ordinal>$2 ORDER BY ordinal LIMIT $3", snapshot, after, limit)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	out.Records = []HistoryRecord{}
	for rows.Next() {
		var raw []byte
		var h HistoryRecord
		if e = rows.Scan(&raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &h); e != nil {
			return out, e
		}
		out.Records = append(out.Records, h)
	}
	return out, rows.Err()
}

func (r Repository) ExportHistory(ctx context.Context, dir, wallet, verifiedWallet, snapshot, format string, now time.Time) (manifest ExportManifest, err error) {
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return manifest, e
	}
	if format != "csv" && format != "jsonl" {
		return manifest, ErrInvalid
	}
	id, e := opaqueID()
	if e != nil {
		return manifest, e
	}
	return r.exportHistoryObject(ctx, dir, wallet, verifiedWallet, snapshot, format, id, now)
}

func (r Repository) exportHistoryObject(ctx context.Context, dir, wallet, verifiedWallet, snapshot, format, id string, now time.Time) (manifest ExportManifest, err error) {
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return manifest, e
	}
	if (format != "csv" && format != "jsonl") || !validUUID(snapshot) || !validExportObjectID(id) {
		return manifest, ErrInvalid
	}
	if e := privateExportDirectory(dir); e != nil {
		return manifest, e
	}
	f, e := os.OpenFile(filepath.Join(dir, id+".partial"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return manifest, e
	}
	partial := f.Name()
	complete := filepath.Join(dir, id+"."+format)
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
	csvWriter := csv.NewWriter(writer)
	if format == "csv" {
		if e = csvWriter.Write([]string{"event_id", "kind", "chain_id", "height", "block_hash", "transaction_hash", "transaction_index", "log_index", "asset_address", "decimals", "amount_raw", "quote_amount_usdl_raw", "basis_quality"}); e != nil {
			return manifest, e
		}
	}
	after := -1
	count := 0
	for {
		if e = ctx.Err(); e != nil {
			return manifest, e
		}
		page, e := r.HistoryPage(ctx, wallet, verifiedWallet, snapshot, after, 100, now)
		if e != nil {
			return manifest, e
		}
		for _, h := range page.Records {
			if format == "jsonl" {
				e = enc.Encode(h)
			} else {
				quote := ""
				if h.QuoteAmountUsdlRaw != nil {
					quote = *h.QuoteAmountUsdlRaw
				}
				decimals, _ := json.Marshal(*h.Asset.Decimals)
				e = csvWriter.Write([]string{h.EventID, h.Kind, h.Position.ChainID, h.Position.Height, h.Position.BlockHash, h.Position.TransactionHash, h.Position.TransactionIndex, h.Position.LogIndex, *h.Asset.Address, string(decimals), h.AmountRaw, quote, h.BasisQuality})
			}
			if e != nil {
				return manifest, e
			}
			count++
		}
		after += len(page.Records)
		if count == page.Rows {
			break
		}
		if len(page.Records) == 0 {
			return manifest, errors.New("history row count mismatch")
		}
	}
	if format == "csv" {
		csvWriter.Flush()
		if e = csvWriter.Error(); e != nil {
			return manifest, e
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
	return ExportManifest{Object: id + "." + format, Rows: count, SHA256: hex.EncodeToString(hash.Sum(nil)), SnapshotID: snapshot, ExpiresAt: now.Add(15 * time.Minute), Format: format}, nil
}

func (r Repository) HistorySeries(ctx context.Context, wallet, verified string, from, to time.Time, interval string, limit int) ([]interfaces.PnlHistoryEntry, *Summary, error) {
	if e := RequireOwner(wallet, verified); e != nil {
		return nil, nil, e
	}
	wallet, _ = Wallet(wallet)
	step := time.Hour
	sqlStep := "1 hour"
	if interval == "1d" {
		step = 24 * time.Hour
		sqlStep = "1 day"
	} else if interval != "1h" {
		return nil, nil, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	if from.IsZero() || !from.Before(to) || limit < 1 || limit > 1000 || to.Sub(from) > time.Duration(limit)*step {
		return nil, nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	admission := r.ReadAdmission
	if admission == nil {
		admission = defaultAdmission
	}
	release, e := admission.Acquire(ctx, wallet)
	if e != nil {
		return nil, nil, e
	}
	defer release()
	rows, e := r.Pool.Query(ctx, `SELECT chosen.summary FROM generate_series($3::timestamptz,$2::timestamptz-interval '1 microsecond',$4::interval) AS buckets(bucket) CROSS JOIN LATERAL (SELECT summary FROM portfolio_snapshots WHERE wallet=$1 AND observed_at>=bucket AND observed_at<LEAST(bucket+$4::interval,$2::timestamptz) ORDER BY observed_at DESC,revision DESC LIMIT 1) chosen ORDER BY bucket LIMIT $5`, wallet, to, from, sqlStep, limit)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	out := []interfaces.PnlHistoryEntry{}
	var last *Summary
	for rows.Next() {
		var raw []byte
		var summary Summary
		if e = rows.Scan(&raw); e != nil {
			return nil, nil, e
		}
		if e = json.Unmarshal(raw, &summary); e != nil {
			return nil, nil, e
		}
		entry := struct {
			Time      string    `json:"time"`
			Watermark Watermark `json:"watermark"`
			Priced    string    `json:"pricedSubtotalUsdlRaw"`
			Complete  *string   `json:"completeValueUsdlRaw"`
			Realized  string    `json:"knownRealizedSubtotalUsdlRaw"`
			Basis     string    `json:"basisQuality"`
			Unpriced  int       `json:"unpricedAssetCount"`
		}{summary.Watermark.ObservedAt, summary.Watermark, summary.PricedSubtotalUsdlRaw, summary.CompleteValueUsdlRaw, summary.KnownRealizedSubtotalUsdlRaw, summary.BasisQuality, summary.UnpricedAssetCount}
		raw, e = json.Marshal(entry)
		if e != nil {
			return nil, nil, e
		}
		if e = interfaces.ValidateJSON("PnlHistoryEntry", raw); e != nil {
			return nil, nil, e
		}
		var item interfaces.PnlHistoryEntry
		if e = json.Unmarshal(raw, &item); e != nil {
			return nil, nil, e
		}
		out = append(out, item)
		copy := summary
		last = &copy
	}
	return out, last, rows.Err()
}
