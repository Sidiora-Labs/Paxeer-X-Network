package projectionstats

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPhase2CompactCursorBindsActualReceiptPosition(t *testing.T) {
	authority, labels := historicalAuthority(t)
	trades := authority.Trades(labels["buy"])
	if len(trades) != 1 {
		t.Fatal("actual receipt required")
	}
	trade := trades[0]
	service := &ReadService{config: ReadConfig{CursorKey: []byte(strings.Repeat("k", 32))}}
	f := Filter{trade.PoolAddress, "buy", "phase2-v1"}
	id := uuid.New()
	encoded, err := service.encodeCompact(f, id, trade.Position)
	if err != nil || len(encoded) > 256 {
		t.Fatal(encoded, err)
	}
	got, position, err := service.decodeCompact(f, encoded)
	if err != nil || got != id || position[0] != trade.Position.Height || position[1] != trade.Position.TransactionIndex || position[2] != trade.Position.LogIndex {
		t.Fatal(got, position, err)
	}
	wrong := f
	wrong.Side = "sell"
	if _, _, err = service.decodeCompact(wrong, encoded); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross filter cursor", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw[40] ^= 1
	if _, _, err = service.decodeCompact(f, base64.RawURLEncoding.EncodeToString(raw)); !errors.Is(err, ErrInvalid) {
		t.Fatal("tampered cursor", err)
	}
	other := &ReadService{config: ReadConfig{CursorKey: []byte(strings.Repeat("z", 32))}}
	if _, _, err = other.decodeCompact(f, encoded); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong server key", err)
	}
	if _, err = NewReadService(nil, ReadConfig{CursorTTL: time.Minute}); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing real repository admitted", err)
	}
}

func TestPhase3SavedWindowIntegrityBoundary(t *testing.T) {
	ctx, repo := repository(t)
	if _, err := repo.pool.Exec(ctx, repo.Phase2MigrationSQL()); err != nil {
		t.Fatal(err)
	}
	config := ReadConfig{SourceSchema: "indexer", SourceStream: "actual-saved-stream", ProjectionVersion: "phase3-saved", CursorKey: []byte(strings.Repeat("k", 32)), CursorTTL: time.Minute}
	service, err := NewReadService(repo, config)
	if err != nil {
		t.Fatal(err)
	}
	authority, labels := historicalAuthority(t)
	trade := authority.Trades(labels["buy"])[0]
	if _, err = repo.ApplyAuthorized(ctx, authority, trade, "phase3-saved", config.ProjectionVersion); err != nil {
		t.Fatal(err)
	}
	observed := authority.admitted[trade.EventID].timestamp
	aggregate, err := repo.AggregateWindow(ctx, config.ProjectionVersion, trade.Position.Height, observed, 3600)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := WindowSnapshot{PoolAddress: trade.PoolAddress, VolumeUSDLRaw: aggregate.VenueVolumeRaw, PoolFillCount: "1", UniqueActors: "1", TraderPopulation: aggregate.TraderPopulation, UserActionUnavailableReason: "qualified_route_identity_unavailable", WindowStart: observed - 3600, WindowEnd: observed, ComputedAt: observed, Watermark: CanonicalWatermark{ChainID: "125", SourceStream: config.SourceStream, Height: trade.Position.Height, BlockHash: trade.Position.BlockHash, ProjectionVersion: config.ProjectionVersion, ComputedAt: observed}, Fees: []CanonicalAssetFee{}}
	for _, fee := range aggregate.Fees {
		snapshot.Fees = append(snapshot.Fees, CanonicalAssetFee{AssetFee: fee, ChainID: "125", FeeType: "trading_fee", QuoteUnavailableReason: "valuation_source_unavailable"})
	}
	for _, mutation := range []string{"valid", "wrong_pool", "wrong_chain", "wrong_height", "wrong_window", "wrong_time", "bad_amount", "actors_exceed_fills", "duplicate_asset", "cross_asset_quote", "missing_disclosure", "unknown_population"} {
		t.Run(mutation, func(t *testing.T) {
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var saved WindowSnapshot
			if err = json.Unmarshal(raw, &saved); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "wrong_pool":
				saved.PoolAddress = token
			case "wrong_chain":
				saved.Watermark.ChainID = "1"
			case "wrong_height":
				saved.Watermark.Height = "-1"
			case "wrong_window":
				saved.WindowStart++
			case "wrong_time":
				saved.ComputedAt++
			case "bad_amount":
				saved.VolumeUSDLRaw = "1e6"
			case "actors_exceed_fills":
				saved.UniqueActors = "2"
			case "duplicate_asset":
				saved.Fees = append(saved.Fees, saved.Fees[0])
			case "cross_asset_quote":
				saved.Fees[0].QuoteValue = ptr("26")
			case "missing_disclosure":
				saved.Fees[0].QuoteUnavailableReason = ""
			case "unknown_population":
				saved.TraderPopulation = "recipient"
			}
			raw, err = json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.pool.Exec(ctx, "INSERT INTO "+repo.schema+".phase2_windows VALUES($1,3600,$2,$3,$4) ON CONFLICT(pool,seconds,projection_version) DO UPDATE SET payload=excluded.payload", trade.PoolAddress, config.ProjectionVersion, observed, raw); err != nil {
				t.Fatal(err)
			}
			got, err := service.LastWindow(ctx, trade.PoolAddress, 3600)
			if mutation != "valid" {
				if !errors.Is(err, ErrIntegrity) {
					t.Fatal("corrupt saved response accepted", mutation, got, err)
				}
				return
			}
			if err != nil || !got.Stale || got.ComputedAt != observed || got.Watermark != snapshot.Watermark || got.VolumeUSDLRaw != "5000000000" || got.Fees[0].Raw != "15000000" {
				t.Fatal(got, err)
			}
		})
	}
}
