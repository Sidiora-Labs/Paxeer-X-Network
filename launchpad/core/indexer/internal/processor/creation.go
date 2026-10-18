package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
)

func qualifiedCreationFacts(ctx context.Context, receipts []*receipt.QualifiedReceipt) ([]store.CreationFact, error) {
	facts := []store.CreationFact{}
	for _, q := range receipts {
		events, err := q.QualifiedEvents(ctx)
		if err != nil {
			return nil, err
		}
		_, _, timestamp, err := q.SourceContext(ctx)
		if err != nil {
			return nil, err
		}
		for _, env := range events {
			if env == nil || env.Event == nil || env.EmitterRole != "SidioraFactory" || env.EventName != "MarketCreated" {
				continue
			}
			args := env.Event.Args
			get := func(key string) (string, error) {
				v, ok := args[key].(string)
				if !ok {
					return "", store.ErrIntegrity
				}
				return v, nil
			}
			f := store.CreationFact{Factory: env.Raw.Emitter, Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(env.Raw.Order.Block, 10), TransactionIndex: strconv.FormatUint(uint64(env.Raw.Order.Transaction), 10), LogIndex: strconv.FormatUint(uint64(env.Raw.Order.Log), 10), TransactionHash: env.Raw.Identity.TransactionHash, BlockHash: env.Raw.Identity.BlockHash}, BlockTimestampSeconds: strconv.FormatInt(timestamp, 10)}
			if f.Token, err = get("token"); err != nil {
				return nil, err
			}
			if f.Pool, err = get("pool"); err != nil {
				return nil, err
			}
			if f.Creator, err = get("creator"); err != nil {
				return nil, err
			}
			if f.Optical, err = get("optical"); err != nil {
				return nil, err
			}
			if f.NFTID, err = get("nftId"); err != nil {
				return nil, err
			}
			f.Provenance, err = q.SourceProvenance(ctx, common.HexToAddress(f.Factory))
			if err != nil {
				return nil, err
			}
			if f.Provenance.ABIRevision != env.ABIRevision {
				return nil, store.ErrIntegrity
			}
			for _, mirror := range events {
				if mirror == nil || mirror.Event == nil || mirror.EmitterRole != "EventEmitter" || mirror.EventName != "TokenDeployed" {
					continue
				}
				m := mirror.Event.Args
				if m["token"] != f.Token || m["pool"] != f.Pool || m["creator"] != f.Creator {
					continue
				}
				if f.Metadata != nil || mirror.Raw.Identity.TransactionHash != f.Position.TransactionHash || mirror.Raw.Identity.BlockHash != f.Position.BlockHash || m["timestamp"] != f.BlockTimestampSeconds || m["blockNumber"] != f.Position.Height {
					return nil, store.ErrIntegrity
				}
				name, nok := m["name"].(string)
				symbol, sok := m["symbol"].(string)
				decimals, dok := m["decimals"].(string)
				supply, supplyOK := m["totalSupply"].(string)
				if !nok || !sok || !dok || !supplyOK {
					return nil, store.ErrIntegrity
				}
				d, parseErr := strconv.ParseUint(decimals, 10, 8)
				if parseErr != nil {
					return nil, parseErr
				}
				if _, parseErr = accounting.Uint(supply); parseErr != nil {
					return nil, parseErr
				}
				proof, proofErr := q.SourceProvenance(ctx, common.HexToAddress(mirror.Raw.Emitter))
				if proofErr != nil {
					return nil, proofErr
				}
				if proof.ABIRevision != mirror.ABIRevision {
					return nil, store.ErrIntegrity
				}
				f.Metadata = &store.TokenCreationMetadata{Emitter: mirror.Raw.Emitter, Name: name, Symbol: symbol, Decimals: uint8(d), TotalSupply: supply, Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(mirror.Raw.Order.Block, 10), TransactionIndex: strconv.FormatUint(uint64(mirror.Raw.Order.Transaction), 10), LogIndex: strconv.FormatUint(uint64(mirror.Raw.Order.Log), 10), TransactionHash: mirror.Raw.Identity.TransactionHash, BlockHash: mirror.Raw.Identity.BlockHash}, Provenance: proof}
			}
			facts = append(facts, f)
		}
	}
	return facts, nil
}

func stageQualifiedSourceFacts(ctx context.Context, c store.CanonicalTx, b store.CanonicalBatch, receipts []*receipt.QualifiedReceipt, durable *publisher.DurableStager) ([]store.OutboxIntent, error) {
	facts, err := qualifiedCreationFacts(ctx, receipts)
	if err != nil {
		return nil, err
	}
	intents := make([]store.OutboxIntent, 0, len(facts))
	for _, f := range facts {
		if err = c.InsertCreation(ctx, f); err != nil {
			return nil, err
		}
		if err = durable.StageCanonicalCreationTx(ctx, c.Tx, f); err != nil {
			return nil, err
		}
		payload, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		index, err := strconv.ParseUint(f.Position.LogIndex, 10, 32)
		if err != nil {
			return nil, err
		}
		intents = append(intents, store.OutboxIntent{TransactionHash: f.Position.TransactionHash, LogIndex: uint32(index), Kind: "canonical.creation.v1", Payload: payload})
	}
	selected := map[string]store.RawRecord{}
	for _, raw := range b.Records {
		selected[raw.TransactionHash+":"+strconv.FormatUint(uint64(raw.LogIndex), 10)] = raw
	}
	for _, q := range receipts {
		logs, err := q.SourceLogs(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range logs {
			if len(l.Topics) != 3 || l.Topics[0] != crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")) {
				continue
			}
			token := strings.ToLower(l.Address.Hex())
			admitted, err := c.HasCanonicalToken(ctx, token)
			if err != nil {
				return nil, err
			}
			if !admitted {
				continue
			}
			if len(l.Data) != 32 || !bytes.Equal(l.Topics[1][:12], make([]byte, 12)) || !bytes.Equal(l.Topics[2][:12], make([]byte, 12)) {
				return nil, store.ErrIntegrity
			}
			txHash := strings.ToLower(l.TxHash.Hex())
			key := txHash + ":" + strconv.FormatUint(uint64(l.Index), 10)
			raw, ok := selected[key]
			if !ok || raw.Emitter != token || raw.BlockHash != strings.ToLower(l.BlockHash.Hex()) || raw.Height < 0 || uint64(raw.Height) != l.BlockNumber || uint(raw.TransactionIndex) != l.TxIndex || !bytes.Equal(raw.Data, l.Data) || len(raw.Topics) != 3 {
				return nil, errors.New("complete admitted token transfer raw record required")
			}
			for i := range l.Topics {
				if raw.Topics[i] != strings.ToLower(l.Topics[i].Hex()) {
					return nil, store.ErrIntegrity
				}
			}
			provenance, err := q.SourceProvenance(ctx, l.Address)
			if err != nil {
				return nil, err
			}
			f := store.TransferFact{Token: token, From: strings.ToLower(common.BytesToAddress(l.Topics[1][12:]).Hex()), To: strings.ToLower(common.BytesToAddress(l.Topics[2][12:]).Hex()), AmountRaw: new(big.Int).SetBytes(l.Data).String(), Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(l.BlockNumber, 10), TransactionIndex: strconv.FormatUint(uint64(l.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(l.Index), 10), TransactionHash: txHash, BlockHash: strings.ToLower(l.BlockHash.Hex())}, Provenance: provenance}
			if err = c.InsertTokenTransfer(ctx, f); err != nil {
				return nil, err
			}
		}
	}
	return intents, nil
}

// errInventoryIneligible marks a fill whose wallet/token partition pnl cannot
// cost-allocate (yet): no plain same-owner attribution, no baseline issued by
// pnl for this generation and source lease, wallet movements other than its
// own same-owner fills, or history beyond the bounded replay. Such a fill
// publishes its market facet only (for now: see inventory.go for the bounded
// deferral); integrity failures of the indexer's own facts still abort the
// canonical commit.
var errInventoryIneligible = errors.New("inventory enrichment ineligible")

// inventoryRefusal is an errInventoryIneligible with a stable reason code (the
// suffix of the indexer_capability_degradation requirement recorded when the
// fill is decided market-only). pending refusals can resolve in a later
// canonical commit (pnl issues the baseline, the movement range completes);
// the others are properties of the wallet's canonical history and never
// change.
type inventoryRefusal struct {
	code    string
	pending bool
}

func (r *inventoryRefusal) Error() string {
	return errInventoryIneligible.Error() + ": " + r.code
}

func (r *inventoryRefusal) Is(target error) bool { return target == errInventoryIneligible }

func inventoryIneligible(code string) error { return &inventoryRefusal{code: code} }

func inventoryPending(code string) error { return &inventoryRefusal{code: code, pending: true} }

// qualifiedInventoryEnrichment derives the inventory enrichment pnl verifies
// (fresh-same-owner-v1 evidence over the issued baseline and the canonical
// movement history) and returns, in source order, the wallet's earlier
// same-owner fills of the token since creation: the partition is publishable
// only while every one of them was published before it.
func qualifiedInventoryEnrichment(ctx context.Context, c store.CanonicalTx, b store.CanonicalBatch, fill accounting.FillV1, generation, version string) (accounting.EnrichmentV1, []string, error) {
	var e accounting.EnrichmentV1
	a := fill.Attribution
	if a.Payer == nil || a.Recipient == nil || *a.Payer != *a.Recipient || a.Quality == "unavailable" || a.TransactionSender != *a.Payer || (a.Trader != nil && *a.Trader != *a.Payer) {
		return e, nil, inventoryIneligible("not-same-owner")
	}
	wallet := *a.Payer
	// Baseline binding: the evidence is built against the baseline pnl issued
	// for THIS commit's source lease (b.Owner, b.Fence), read inside the fenced
	// canonical transaction. The evidence digest commits to baseline_digest,
	// the hash of pnl's attestation, which names SourceOwner and SourceEpoch,
	// so every frame names the (owner, fence, digest) it was built against.
	// pnl's verifier (core/pnl-tracker l16 baseline.go qualifyFreshTx) selects
	// that row by the evidence digest and accepts it while its epoch is not
	// beyond the current source fence: frames in flight across an indexer
	// fence change stay verifiable, a baseline of a lease the indexer never
	// committed under cannot match.
	var baselineDigest, creationHash, movementDigest, observed string
	var creationHeight int64
	err := c.Tx.QueryRow(ctx, `SELECT baseline_digest,creation_height,creation_block_hash,movement_digest,observed_balance_raw FROM pnl.inventory_l16_baselines_v1 WHERE projection_generation=$1 AND wallet=$2 AND token=$3 AND source_stream=$4 AND source_owner=$5 AND source_epoch=$6 ORDER BY baseline_digest LIMIT 1`, generation, wallet, fill.Token.Address, b.StreamID, b.Owner, b.Fence).Scan(&baselineDigest, &creationHeight, &creationHash, &movementDigest, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, nil, inventoryPending("baseline-unissued")
	}
	if err != nil {
		return e, nil, fmt.Errorf("issued zero movement baseline: %w", err)
	}
	if observed != "0" || len(baselineDigest) != 64 || len(movementDigest) != 64 {
		return e, nil, store.ErrIntegrity
	}
	height, err := strconv.ParseInt(fill.Position.Height, 10, 64)
	if err != nil || height < creationHeight {
		return e, nil, store.ErrIntegrity
	}
	var canonical bool
	err = c.Tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_canonical_creations WHERE token=$1 AND height=$2 AND block_hash=$3) AND (SELECT count(*) FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$4 AND height BETWEEN $2 AND $5 AND fetch_complete AND required_decode_complete)=$5-$2+1`, fill.Token.Address, creationHeight, creationHash, b.StreamID, height).Scan(&canonical)
	if err != nil {
		return e, nil, err
	}
	if !canonical {
		return e, nil, inventoryPending("movement-range-incomplete")
	}
	rows, err := c.Tx.Query(ctx, `SELECT payload FROM indexer.indexer_accounting_fills WHERE payload->'token'->>'address'=$1 AND (payload->'position'->>'height')::bigint BETWEEN $2 AND $3 ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint LIMIT 65537`, fill.Token.Address, creationHeight, height)
	if err != nil {
		return e, nil, err
	}
	expected := map[string]store.TransferFact{}
	prior := []string{}
	sourceCount, sourceBytes := 0, 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return e, nil, err
		}
		sourceCount++
		sourceBytes += len(raw)
		if sourceCount > 65536 || sourceBytes > 32<<20 {
			rows.Close()
			return e, nil, inventoryIneligible("source-replay-bound")
		}
		source, decodeErr := accounting.DecodeFillV1(raw)
		if decodeErr != nil {
			rows.Close()
			return e, nil, decodeErr
		}
		actor := source.Attribution
		if actor.Payer == nil || actor.Recipient == nil || *actor.Payer != wallet || *actor.Recipient != wallet {
			continue
		}
		if actor.TransactionSender != wallet || (actor.Trader != nil && *actor.Trader != wallet) {
			rows.Close()
			return e, nil, inventoryIneligible("routed-wallet-fill")
		}
		order, compareErr := accounting.CompareSourcePosition(source.Position, fill.Position)
		if compareErr != nil {
			rows.Close()
			return e, nil, compareErr
		}
		if order < 0 {
			prior = append(prior, source.SourceFillID)
		}
		kind, from, to := "output-transfer", source.PoolAddress, wallet
		if source.Side == "sell" {
			kind, from, to = "input-transfer", wallet, source.PoolAddress
		}
		matches := 0
		for _, proof := range actor.Evidence {
			if proof.Kind != kind {
				continue
			}
			matches++
			if proof.LogIndex == nil || proof.ArtifactID != source.Position.TransactionHash || proof.ArtifactSHA256 != source.Provenance.ReceiptSHA256 {
				rows.Close()
				return e, nil, store.ErrIntegrity
			}
			key := proof.ArtifactID + ":" + *proof.LogIndex
			if _, duplicate := expected[key]; duplicate {
				rows.Close()
				return e, nil, store.ErrIntegrity
			}
			expected[key] = store.TransferFact{Token: source.Token.Address, From: from, To: to, AmountRaw: source.TokenAmountRaw}
		}
		if matches != 1 {
			rows.Close()
			return e, nil, inventoryIneligible("movement-evidence")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return e, nil, err
	}
	moves, err := c.Tx.Query(ctx, `SELECT payload FROM indexer.indexer_token_transfers WHERE token=$1 AND height BETWEEN $2 AND $3 AND (from_address=$4 OR to_address=$4) ORDER BY height,transaction_index,log_index LIMIT 65537`, fill.Token.Address, creationHeight, height, wallet)
	if err != nil {
		return e, nil, err
	}
	movementPayload := make([]json.RawMessage, 0)
	movementBytesTotal := 0
	for moves.Next() {
		var raw []byte
		if err = moves.Scan(&raw); err != nil {
			moves.Close()
			return e, nil, err
		}
		movementBytesTotal += len(raw)
		if len(movementPayload) >= 65536 || movementBytesTotal > 32<<20 {
			moves.Close()
			return e, nil, inventoryIneligible("movement-replay-bound")
		}
		var actual store.TransferFact
		if err = json.Unmarshal(raw, &actual); err != nil {
			moves.Close()
			return e, nil, err
		}
		key := actual.Position.TransactionHash + ":" + actual.Position.LogIndex
		wanted, ok := expected[key]
		if !ok || actual.Token != wanted.Token || actual.From != wanted.From || actual.To != wanted.To || actual.AmountRaw != wanted.AmountRaw {
			moves.Close()
			return e, nil, inventoryIneligible("unresolved-movement")
		}
		delete(expected, key)
		movementPayload = append(movementPayload, append(json.RawMessage(nil), raw...))
	}
	moves.Close()
	if err = moves.Err(); err != nil {
		return e, nil, err
	}
	if len(expected) != 0 {
		return e, nil, errors.New("qualified settlement transfer missing from canonical movement facts")
	}
	original, err := accounting.FillDigest(fill)
	if err != nil {
		return e, nil, err
	}
	movementBytes, err := json.Marshal(movementPayload)
	if err != nil {
		return e, nil, err
	}
	evidence := accounting.Digest(accounting.HashTuple("fresh-inventory-evidence-v1", baselineDigest, movementDigest, string(original), string(movementBytes)))
	e = accounting.EnrichmentV1{SourceFillID: fill.SourceFillID, SourceFactDigest: original, Facet: "inventory", EnrichmentVersion: "fresh-same-owner-v1", ProjectionVersion: version, EvidenceDigest: evidence, Attribution: &a}
	if err = accounting.ValidateEnrichmentV1(e); err != nil {
		return e, nil, err
	}
	return e, prior, nil
}

// inventoryPartitionContiguous reports whether publishing fill keeps the
// wallet/token inventory partition gap-free for pnl, which bootstraps a
// partition at sequence 1 from its creation baseline and replays every leg:
// the fills already appended to the partition before fill, plus those planned
// earlier in this commit, must be exactly the wallet's earlier same-owner
// fills. A partition is never started or resumed mid-history: a held first
// fill is published late (inventory.go deferral rule), never skipped. Positions
// recorded for fills the wallet's history no longer contains are an integrity
// failure.
func inventoryPartitionContiguous(ctx context.Context, tx pgx.Tx, version, key string, fill accounting.FillV1, prior []string, planned map[string]bool) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT position FROM indexer.delivery_partition_positions WHERE projection_version=$1 AND partition_kind='inventory' AND partition_key=$2 ORDER BY sequence LIMIT 65537`, version, key)
	if err != nil {
		return false, err
	}
	published := map[string]bool{}
	for id := range planned {
		published[id] = true
	}
	count := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return false, err
		}
		if count++; count > 65536 {
			rows.Close()
			return false, inventoryIneligible("partition-bound")
		}
		var position accounting.PartitionPosition
		if err = json.Unmarshal(raw, &position); err != nil {
			rows.Close()
			return false, store.ErrIntegrity
		}
		order, compareErr := accounting.CompareSourcePosition(position.SourceOrder, fill.Position)
		if compareErr != nil {
			rows.Close()
			return false, compareErr
		}
		if order < 0 {
			published[position.SourceEventID] = true
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	earlier := map[string]bool{}
	for _, id := range prior {
		earlier[id] = true
	}
	for id := range published {
		if !earlier[id] {
			return false, store.ErrIntegrity
		}
	}
	return len(published) == len(earlier), nil
}
