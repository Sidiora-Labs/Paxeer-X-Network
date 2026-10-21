package l16

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrParity = errors.New("inventory committed-node parity disagreement; source quarantined")

type RebuildSnapshot struct {
	Generation      string       `json:"generation"`
	Partition       string       `json:"partition"`
	Algorithm       string       `json:"algorithm"`
	ReleaseDigest   string       `json:"releaseDigest"`
	CreationHeight  int64        `json:"creationHeight"`
	Height          int64        `json:"height"`
	BlockHash       string       `json:"blockHash"`
	MovementDigest  string       `json:"movementDigest"`
	ObservedThrough time.Time    `json:"observedThrough"`
	SourceOwner     string       `json:"sourceOwner"`
	SourceEpoch     int64        `json:"sourceEpoch"`
	NodeIDs         [2]string    `json:"nodeIds"`
	NodeBalances    [2]string    `json:"nodeBalances"`
	State           Inventory    `json:"state"`
	LastIdentity    string       `json:"lastIdentity"`
	Checkpoints     []Checkpoint `json:"checkpoints"`
	InputDigest     string       `json:"inputDigest"`
}

type Service struct {
	authority *BaselineAuthority
	second    *ethclient.Client
	nodeIDs   [2]string
	release   string
	outbox    *delivery.Store
}

func NewService(authority *BaselineAuthority, second *ethclient.Client, nodeIDs [2]string, releaseDigest string, outbox *delivery.Store) (*Service, error) {
	if authority == nil || authority.pool == nil || authority.rpc == nil || authority.epochGuard == nil || second == nil || second == authority.rpc || nodeIDs[0] == "" || nodeIDs[1] == "" || nodeIDs[0] == nodeIDs[1] || len(nodeIDs[0]) > 256 || len(nodeIDs[1]) > 256 || !digestHex(releaseDigest) || outbox == nil {
		return nil, ErrBaselineUnavailable
	}
	return &Service{authority: authority, second: second, nodeIDs: nodeIDs, release: releaseDigest, outbox: outbox}, nil
}

func digestHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Service) Rebuild(ctx context.Context, wallet, token string, end int64) (RebuildSnapshot, error) {
	return s.rebuild(ctx, wallet, token, end, false)
}
func (s *Service) RebuildArchive(ctx context.Context, wallet, token string, end int64) (RebuildSnapshot, error) {
	return s.rebuild(ctx, wallet, token, end, true)
}
func (s *Service) rebuild(ctx context.Context, wallet, token string, end int64, paged bool) (RebuildSnapshot, error) {
	var snapshot RebuildSnapshot
	if !address.MatchString(wallet) || !address.MatchString(token) || wallet == zeroAddress || token == zeroAddress || end <= 0 {
		return snapshot, ErrBaselineUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a := s.authority
	tx, err := a.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return snapshot, err
	}
	defer inventoryRollback(tx)
	if err = a.epochGuard(bounded, tx); err != nil {
		return snapshot, err
	}
	partition := "125:" + wallet + ":" + token
	if _, err = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portfolio:"+a.generation+":"+wallet); err != nil {
		return snapshot, err
	}
	if _, err = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, partition); err != nil {
		return snapshot, err
	}
	var quarantined bool
	if err = tx.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM `+a.table("inventory_l16_quarantine_v2")+` WHERE generation=$1 AND partition=$2 AND height<=$3)`, a.generation, partition, end).Scan(&quarantined); err != nil {
		return snapshot, err
	}
	if quarantined {
		return snapshot, ErrParity
	}
	owner, epoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return snapshot, err
	}
	var start int64
	var creationHash, pool, creationTx string
	if err = tx.QueryRow(bounded, `SELECT height,block_hash,pool,transaction_hash FROM indexer.indexer_canonical_creations_v1 WHERE token=$1`, token).Scan(&start, &creationHash, &pool, &creationTx); err != nil {
		return snapshot, err
	}
	if start <= 0 || end < start || !paged && end-start > 10000 {
		return snapshot, ErrBaselineUnavailable
	}
	code, err := a.rpc.CodeAt(bounded, common.HexToAddress(token), big.NewInt(start-1))
	if err != nil {
		return snapshot, err
	}
	if len(code) != 0 {
		return snapshot, ErrBaselineUnavailable
	}
	code, err = a.rpc.CodeAtHash(bounded, common.HexToAddress(token), common.HexToHash(creationHash))
	if err != nil {
		return snapshot, err
	}
	if len(code) == 0 {
		return snapshot, ErrBaselineUnavailable
	}
	var moves []movement
	var movementDigest, endHash string
	if paged {
		moves, movementDigest, endHash, err = a.verifiedMovementPages(bounded, tx, token, start, end)
	} else {
		moves, movementDigest, endHash, err = a.verifiedMovements(bounded, tx, token, start, end)
	}
	if err != nil {
		return snapshot, err
	}
	creation, creationReported, err := reportedHeaderByNumber(bounded, a.rpc, big.NewInt(start))
	if err != nil {
		return snapshot, err
	}
	if creationReported != creationHash {
		return snapshot, delivery.ErrIntegrity
	}
	if err = rebuildCreationPath(moves, start, creationHash, creationTx, a.factory, pool); err != nil {
		return snapshot, err
	}
	rows, err := tx.Query(bounded, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE (payload->'position'->>'height')::bigint BETWEEN $1 AND $2 AND payload->'token'->>'address'=$3 ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint`, start, end, token)
	if err != nil {
		return snapshot, err
	}
	var fills []accounting.FillV1
	for rows.Next() {
		var raw []byte
		var digest string
		if err = rows.Scan(&raw, &digest); err != nil {
			rows.Close()
			return snapshot, err
		}
		fill, e := accounting.DecodeFillV1(raw)
		if e != nil {
			rows.Close()
			return snapshot, e
		}
		d, e := accounting.FillDigest(fill)
		if e != nil || string(d) != digest {
			rows.Close()
			return snapshot, delivery.ErrIntegrity
		}
		fills = append(fills, fill)
		if !paged && len(fills) > 10000 || len(fills) > 1000000 {
			rows.Close()
			return snapshot, ErrBaselineUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return snapshot, err
	}
	settlements := map[string]receiptSettlement{}
	for _, fill := range fills {
		settlement, e := a.receiptSettlement(bounded, tx, fill)
		if e != nil {
			return snapshot, e
		}
		key := settlement.tokenMovement.TransactionHash + ":" + strconv.FormatUint(uint64(settlement.tokenMovement.LogIndex), 10)
		if _, ok := settlements[key]; ok {
			return snapshot, delivery.ErrIntegrity
		}
		settlements[key] = settlement
	}
	contractAddress := common.HexToAddress(token)
	decimalRaw, e := a.rpc.CallContractAtHash(bounded, ethereum.CallMsg{To: &contractAddress, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}, common.HexToHash(endHash))
	if e != nil {
		return snapshot, e
	}
	if len(decimalRaw) != 32 || new(big.Int).SetBytes(decimalRaw).Cmp(big.NewInt(255)) > 0 {
		return snapshot, delivery.ErrIntegrity
	}
	tokenDecimals := int(new(big.Int).SetBytes(decimalRaw).Int64())
	records := make([]EconomicRecord, 0)
	state := Empty()
	last := ""
	headers := map[string]time.Time{}
	for _, m := range moves {
		if m.From != wallet && m.To != wallet {
			continue
		}
		at, ok := headers[m.BlockHash]
		if !ok {
			h, e := a.rpc.HeaderByHash(bounded, common.HexToHash(m.BlockHash))
			if e != nil {
				return snapshot, e
			}
			if h == nil || h.Number == nil || h.Number.Int64() != m.Height || h.Time > 1<<63-1 {
				return snapshot, delivery.ErrIntegrity
			}
			at = time.Unix(int64(h.Time), 0).UTC()
			headers[m.BlockHash] = at
		}
		key := m.TransactionHash + ":" + strconv.FormatUint(uint64(m.LogIndex), 10)
		var settlement *receiptSettlement
		if found, ok := settlements[key]; ok {
			copy := found
			settlement = &copy
		}
		movementBytes, e := json.Marshal(m)
		if e != nil {
			return snapshot, e
		}
		record, include, e := normalizeMovement(wallet, token, m, settlement, at, delivery.Hash(movementBytes))
		if e != nil {
			return snapshot, e
		}
		if !include {
			continue
		}
		record.TokenDecimals = &tokenDecimals
		record.Leg.Predecessor = last
		if record.Leg.Kind == "sell" || record.Leg.Kind == "outgoing" || record.Leg.Kind == "burn" {
			var allocation Disposal
			state, allocation, e = Dispose(state, record.Leg.Quantity, record.Leg.ActualUSDL)
			record.Allocation = &allocation
		} else {
			state, e = Apply(state, record.Leg)
		}
		if e != nil {
			return snapshot, e
		}
		record.State = state
		last, e = record.Leg.Identity.Key()
		if e != nil {
			return snapshot, e
		}
		records = append(records, record)
		if !paged && len(records) > 10000 || len(records) > 1000000 {
			return snapshot, ErrBaselineUnavailable
		}
	}
	firstBalance, err := a.observedBalance(bounded, token, wallet, endHash)
	if err != nil {
		return snapshot, err
	}
	chain, err := s.second.ChainID(bounded)
	if err != nil {
		return snapshot, err
	}
	if chain.Cmp(big.NewInt(125)) != 0 {
		return snapshot, delivery.ErrIntegrity
	}
	_, otherHash, err := reportedHeaderByNumber(bounded, s.second, big.NewInt(end))
	if err != nil {
		return snapshot, err
	}
	data := append(append([]byte(nil), balanceSelector...), common.LeftPadBytes(common.HexToAddress(wallet).Bytes(), 32)...)
	contract := common.HexToAddress(token)
	rawBalance, err := s.second.CallContractAtHash(bounded, ethereum.CallMsg{To: &contract, Data: data}, common.HexToHash(endHash))
	if err != nil {
		return snapshot, err
	}
	if len(rawBalance) != 32 {
		return snapshot, delivery.ErrIntegrity
	}
	secondBalance := new(big.Int).SetBytes(rawBalance)
	values, err := state.values()
	if err != nil {
		return snapshot, err
	}
	snapshot = RebuildSnapshot{Generation: a.generation, Partition: partition, Algorithm: Algorithm, ReleaseDigest: s.release, CreationHeight: start, Height: end, BlockHash: endHash, MovementDigest: movementDigest, SourceOwner: owner, SourceEpoch: epoch, NodeIDs: s.nodeIDs, NodeBalances: [2]string{firstBalance.String(), secondBalance.String()}, State: state, LastIdentity: last}
	if otherHash != endHash || firstBalance.Cmp(secondBalance) != 0 || firstBalance.Cmp(add(values[0], values[1])) != 0 {
		payload, _ := json.Marshal(snapshot)
		if _, err = tx.Exec(bounded, `INSERT INTO `+a.table("inventory_l16_quarantine_v2")+` (generation,partition,height,block_hash,reason,payload) VALUES ($1,$2,$3,$4,'dual_node_or_inventory_parity',$5) ON CONFLICT DO NOTHING`, a.generation, partition, end, endHash, payload); err != nil {
			return snapshot, err
		}
		if err = a.epochGuard(bounded, tx); err != nil {
			return snapshot, err
		}
		if err = tx.Commit(bounded); err != nil {
			return snapshot, err
		}
		return snapshot, ErrParity
	}
	endHeader, err := a.rpc.HeaderByHash(bounded, common.HexToHash(endHash))
	if err != nil {
		return snapshot, err
	}
	if endHeader == nil || endHeader.Time > 1<<63-1 || creation.Time > 1<<63-1 {
		return snapshot, delivery.ErrIntegrity
	}
	snapshot.ObservedThrough = time.Unix(int64(endHeader.Time), 0).UTC()
	legs := make([]TimedLeg, len(records))
	for i := range records {
		legs[i] = records[i].TimedLeg
	}
	day := time.Unix(int64(creation.Time), 0).UTC()
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Unix(int64(endHeader.Time), 0).UTC()
	endDay = time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 0, 0, 0, 0, time.UTC)
	if endDay.Before(day) || endDay.Sub(day) > 3660*24*time.Hour {
		return snapshot, ErrBaselineUnavailable
	}
	var previous Checkpoint
	for !day.After(endDay) {
		var cp Checkpoint
		daily := legs
		if paged {
			daily = make([]TimedLeg, 0)
			next := day.AddDate(0, 0, 1)
			for _, entry := range legs {
				if !entry.CommittedAt.Before(day) && entry.CommittedAt.Before(next) {
					daily = append(daily, entry)
				}
			}
		}
		if paged {
			cp, err = RebuildDayPages(bounded, previous, daily, day)
		} else if len(snapshot.Checkpoints) == 0 {
			cp, err = RebuildDay(bounded, Empty(), daily, day, true)
		} else {
			cp, err = ContinueDay(bounded, previous, daily, day)
		}
		if err != nil {
			return snapshot, err
		}
		snapshot.Checkpoints = append(snapshot.Checkpoints, cp)
		previous = cp
		day = day.AddDate(0, 0, 1)
	}
	if previous.State != state {
		return snapshot, delivery.ErrIntegrity
	}
	input, _ := json.Marshal(records)
	snapshot.InputDigest = delivery.Hash(input)
	finalOwner, finalEpoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return snapshot, err
	}
	if finalOwner != owner || finalEpoch != epoch {
		return snapshot, delivery.ErrLease
	}
	if err = s.persistReplayTx(bounded, tx, snapshot, records); err != nil {
		return snapshot, err
	}
	if err = a.epochGuard(bounded, tx); err != nil {
		return snapshot, err
	}
	if err = tx.Commit(bounded); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (s *Service) persistReplayTx(ctx context.Context, tx pgx.Tx, snapshot RebuildSnapshot, records []EconomicRecord) error {
	a := s.authority
	for _, record := range records {
		identity, err := record.Leg.Identity.Key()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		digest := delivery.Hash(raw)
		if _, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_archive_v2")+` (generation,partition,identity,payload_hash,payload) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, identity, digest, raw); err != nil {
			return err
		}
		var old string
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM `+a.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2 AND identity=$3`, a.generation, snapshot.Partition, identity).Scan(&old); err != nil {
			return err
		}
		if old != digest {
			return delivery.ErrIntegrity
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	digest := delivery.Hash(raw)
	if _, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_snapshots_v2")+` (generation,partition,snapshot_digest,payload,height,block_hash,release_digest) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, digest, raw, snapshot.Height, snapshot.BlockHash, snapshot.ReleaseDigest); err != nil {
		return err
	}
	for _, cp := range snapshot.Checkpoints {
		payload, e := json.Marshal(struct {
			Checkpoint    Checkpoint `json:"checkpoint"`
			ReleaseDigest string     `json:"releaseDigest"`
			InputDigest   string     `json:"inputDigest"`
		}{cp, snapshot.ReleaseDigest, snapshot.InputDigest})
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_checkpoints_v2")+` (generation,partition,day,snapshot_digest,checkpoint_digest,payload) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, cp.Day, digest, delivery.Hash(payload), payload); e != nil {
			return e
		}
	}
	var currentHeight int64
	var currentGeneration, currentDigest string
	err = tx.QueryRow(ctx, `SELECT height,generation,snapshot_digest FROM `+a.table("inventory_l16_active_v2")+` WHERE partition=$1 FOR UPDATE`, snapshot.Partition).Scan(&currentHeight, &currentGeneration, &currentDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && currentHeight > snapshot.Height {
		return delivery.ErrIntegrity
	}
	if err == nil && currentHeight == snapshot.Height && currentDigest != digest {
		var priorRaw []byte
		if e := tx.QueryRow(ctx, `SELECT payload FROM `+a.table("inventory_l16_snapshots_v2")+` WHERE generation=$1 AND partition=$2 AND snapshot_digest=$3`, currentGeneration, snapshot.Partition, currentDigest).Scan(&priorRaw); e != nil {
			return e
		}
		var prior RebuildSnapshot
		if delivery.Hash(priorRaw) != currentDigest || json.Unmarshal(priorRaw, &prior) != nil || currentGeneration != a.generation || !sameReplaySnapshot(prior, snapshot) {
			return delivery.ErrIntegrity
		}
	}
	now := time.Now().UTC()
	envelope := delivery.Envelope{EventID: digest, EventType: RebuildSubject, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(snapshot.Height, 10), TransactionIndex: "0", LogIndex: "0"}, EmittedAt: now, TraceID: digest, PayloadHash: digest, Payload: raw}
	if err == nil && currentDigest == digest {
		return nil
	}
	if err = s.outbox.Enqueue(ctx, tx, RebuildSubject, envelope); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_active_v2")+` (partition,generation,snapshot_digest,height) VALUES ($1,$2,$3,$4) ON CONFLICT (partition) DO UPDATE SET generation=EXCLUDED.generation,snapshot_digest=EXCLUDED.snapshot_digest,height=EXCLUDED.height`, snapshot.Partition, a.generation, digest, snapshot.Height)
	return err
}

func (s *Service) Active(ctx context.Context, wallet, token string) (RebuildSnapshot, error) {
	var result RebuildSnapshot
	if !address.MatchString(wallet) || !address.MatchString(token) {
		return result, ErrBaselineUnavailable
	}
	var raw []byte
	var digest string
	err := s.authority.pool.QueryRow(ctx, `SELECT p.payload,p.snapshot_digest FROM `+s.authority.table("inventory_l16_active_v2")+` a JOIN `+s.authority.table("inventory_l16_snapshots_v2")+` p USING(generation,partition,snapshot_digest) WHERE a.partition=$1`, "125:"+wallet+":"+token).Scan(&raw, &digest)
	if err != nil {
		return result, err
	}
	if delivery.Hash(raw) != digest {
		return result, delivery.ErrIntegrity
	}
	err = json.Unmarshal(raw, &result)
	if err != nil {
		return result, err
	}
	if _, err = result.State.values(); err != nil {
		return RebuildSnapshot{}, err
	}
	if result.Partition != "125:"+wallet+":"+token || result.Algorithm != Algorithm || !regexpToken(result.Generation) || result.Height <= 0 || !hash.MatchString(result.BlockHash) || !digestHex(result.ReleaseDigest) {
		return RebuildSnapshot{}, delivery.ErrIntegrity
	}
	return result, nil
}

func (s *Service) HistoricalDay(ctx context.Context, wallet, token string, day time.Time) (Checkpoint, error) {
	snapshot, err := s.Active(ctx, wallet, token)
	if err != nil {
		return Checkpoint{}, err
	}
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if snapshot.ObservedThrough.Before(start.AddDate(0, 0, 1)) {
		return Checkpoint{}, ErrBaselineUnavailable
	}
	for _, checkpoint := range snapshot.Checkpoints {
		if checkpoint.Day == start.Format("2006-01-02") {
			if !checkpoint.CompleteHistory {
				return Checkpoint{}, ErrBaselineUnavailable
			}
			return checkpoint, nil
		}
	}
	return Checkpoint{}, ErrBaselineUnavailable
}

func (a *BaselineAuthority) verifiedMovementPages(ctx context.Context, tx pgx.Tx, token string, start, end int64) ([]movement, string, string, error) {
	var all []movement
	type pageProof struct {
		Start, End   int64
		Digest, Hash string
	}
	proofs := []pageProof{}
	lastHash := ""
	for next := start; next <= end; {
		if err := ctx.Err(); err != nil {
			return nil, "", "", err
		}
		pageEnd := next + 10000
		if pageEnd < next || pageEnd > end {
			pageEnd = end
		}
		moves, digest, hash, err := a.verifiedMovements(ctx, tx, token, next, pageEnd)
		if err != nil {
			return nil, "", "", err
		}
		if len(all)+len(moves) > 1000000 {
			return nil, "", "", ErrBaselineUnavailable
		}
		all = append(all, moves...)
		proofs = append(proofs, pageProof{next, pageEnd, digest, hash})
		lastHash = hash
		if pageEnd == end {
			break
		}
		next = pageEnd + 1
	}
	raw, err := json.Marshal(proofs)
	if err != nil {
		return nil, "", "", err
	}
	return all, delivery.Hash(raw), lastHash, nil
}

func (s *Service) PublishRebuiltWallet(ctx context.Context, wallet string, head int64, repository l17.Repository) (l17.Summary, error) {
	var summary l17.Summary
	if !address.MatchString(wallet) || head <= 0 || repository.Pool != s.authority.pool {
		return summary, ErrBaselineUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a := s.authority
	tx, err := a.pool.Begin(bounded)
	if err != nil {
		return summary, err
	}
	defer inventoryRollback(tx)
	if err = a.epochGuard(bounded, tx); err != nil {
		return summary, err
	}
	if _, err = tx.Exec(bounded, `SET LOCAL search_path TO `+a.schema+`,pg_catalog`); err != nil {
		return summary, err
	}
	if _, err = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portfolio:"+a.generation+":"+wallet); err != nil {
		return summary, err
	}
	prefix := "125:" + wallet + ":"
	var missing bool
	if err = tx.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM inventory_l16_effects e WHERE e.projection_generation=$1 AND left(e.partition,length($2))=$2 AND NOT EXISTS(SELECT 1 FROM inventory_l16_active_v2 a WHERE a.partition=e.partition AND a.generation=$1 AND a.height=$3))`, a.generation, prefix, head).Scan(&missing); err != nil {
		return summary, err
	}
	if missing {
		return summary, delivery.ErrGap
	}
	var quarantined bool
	if err = tx.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM inventory_l16_quarantine_v2 WHERE generation=$1 AND left(partition,length($2))=$2 AND height<=$3)`, a.generation, prefix, head).Scan(&quarantined); err != nil {
		return summary, err
	}
	if quarantined {
		return summary, ErrParity
	}
	rows, err := tx.Query(bounded, `SELECT p.snapshot_digest,p.payload FROM inventory_l16_active_v2 a JOIN inventory_l16_snapshots_v2 p USING(generation,partition,snapshot_digest) WHERE left(a.partition,length($1))=$1 ORDER BY a.partition`, prefix)
	if err != nil {
		return summary, err
	}
	snapshots := []RebuildSnapshot{}
	digests := []string{}
	for rows.Next() {
		var digest string
		var raw []byte
		if err = rows.Scan(&digest, &raw); err != nil {
			rows.Close()
			return summary, err
		}
		if delivery.Hash(raw) != digest {
			rows.Close()
			return summary, delivery.ErrIntegrity
		}
		var snapshot RebuildSnapshot
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			rows.Close()
			return summary, err
		}
		if snapshot.Generation != a.generation || snapshot.Height != head || snapshot.ReleaseDigest != s.release {
			rows.Close()
			return summary, delivery.ErrGap
		}
		snapshots = append(snapshots, snapshot)
		digests = append(digests, digest)
		if len(snapshots) > 100000 {
			rows.Close()
			return summary, ErrCostAllocationUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return summary, err
	}
	if len(snapshots) == 0 {
		return summary, ErrBaselineUnavailable
	}
	raw, err := json.Marshal(struct {
		Generation, Wallet string
		Snapshots          []string
	}{a.generation, wallet, digests})
	if err != nil {
		return summary, err
	}
	source := delivery.Hash(raw)
	var prior []byte
	err = tx.QueryRow(bounded, `SELECT summary FROM portfolio_snapshots WHERE source_event=$1`, source).Scan(&prior)
	if err == nil {
		if err = json.Unmarshal(prior, &summary); err != nil {
			return summary, err
		}
		return summary, tx.Commit(bounded)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return summary, err
	}
	var lastRevision string
	if err = tx.QueryRow(bounded, `SELECT COALESCE(max(revision),0)::text FROM portfolio_snapshots WHERE wallet=$1`, wallet).Scan(&lastRevision); err != nil {
		return summary, err
	}
	revision, err := Uint256(lastRevision)
	if err != nil {
		return summary, err
	}
	revision.Add(revision, big.NewInt(1))
	if revision.BitLen() > 256 {
		return summary, delivery.ErrIntegrity
	}
	headSnapshot := snapshots[0]
	watermark := l17.Watermark{ChainID: "125", Height: strconv.FormatInt(head, 10), BlockHash: headSnapshot.BlockHash, ProjectionRevision: revision.String(), ObservedAt: headSnapshot.ObservedThrough.UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	positions := make([]l17.Position, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.BlockHash != headSnapshot.BlockHash || !snapshot.ObservedThrough.Equal(headSnapshot.ObservedThrough) {
			return summary, delivery.ErrIntegrity
		}
		parts := strings.Split(snapshot.Partition, ":")
		if len(parts) != 3 || parts[1] != wallet {
			return summary, delivery.ErrIntegrity
		}
		position, e := ProjectPosition(wallet, parts[2], snapshot.State, watermark, nil, "unpriced")
		if e != nil {
			return summary, e
		}
		positions = append(positions, position)
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("rebuilt-wallet:"+source)).String()
	summary, err = l17.Aggregate(wallet, id, watermark, positions)
	if err != nil {
		return summary, err
	}
	notice := l17.Notice{Wallet: wallet, SnapshotID: id, Revision: revision.String(), Algorithm: Algorithm}
	payload, err := json.Marshal(notice)
	if err != nil {
		return summary, err
	}
	envelope := delivery.Envelope{EventID: source, EventType: "portfolio.snapshot.v2", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(head, 10), TransactionIndex: "0", LogIndex: "0"}, EmittedAt: headSnapshot.ObservedThrough.UTC(), TraceID: source, PayloadHash: delivery.Hash(payload), Payload: payload}
	if summary, err = repository.CommitFullProjectionDeliveryTx(bounded, tx, s.outbox, "portfolio.snapshot.v2", source, summary, positions, time.Now().UTC().Add(24*time.Hour), envelope); err != nil {
		return summary, err
	}
	if err = s.commitRebuiltHistoryTx(bounded, tx, wallet, id, source, snapshots, repository); err != nil {
		return summary, err
	}
	if err = a.epochGuard(bounded, tx); err != nil {
		return summary, err
	}
	return summary, tx.Commit(bounded)
}

type IntervalFee struct {
	Asset     l17.AssetRef `json:"asset"`
	AmountRaw string       `json:"amountRaw"`
}
type IntervalPnL struct {
	Start                      time.Time     `json:"start"`
	End                        time.Time     `json:"end"`
	GrossProceedsUsdlRaw       string        `json:"grossProceedsUsdlRaw"`
	KnownAllocatedBasisUsdlRaw string        `json:"knownAllocatedBasisUsdlRaw"`
	KnownNetGainUsdlRaw        string        `json:"knownNetGainUsdlRaw"`
	UnknownProceedsUsdlRaw     string        `json:"unknownProceedsUsdlRaw"`
	RealizedPnlUsdlRaw         *string       `json:"realizedPnlUsdlRaw"`
	TradeCount                 int           `json:"tradeCount"`
	SaleCount                  int           `json:"saleCount"`
	Fees                       []IntervalFee `json:"fees"`
	GasPaxRaw                  string        `json:"gasPaxRaw"`
	AccountingComplete         bool          `json:"accountingComplete"`
}
type IntervalSeries struct {
	Version       int           `json:"version"`
	Algorithm     string        `json:"algorithm"`
	Method        string        `json:"method"`
	SnapshotID    string        `json:"snapshotId"`
	Wallet        string        `json:"wallet"`
	ReleaseDigest string        `json:"releaseDigest"`
	InputDigest   string        `json:"inputDigest"`
	Watermark     l17.Watermark `json:"watermark"`
	Entries       []IntervalPnL `json:"entries"`
}

func intervalRecords(records []EconomicRecord, step time.Duration) ([]IntervalPnL, error) {
	if step != time.Hour && step != 24*time.Hour {
		return nil, errors.New("unsupported inventory interval")
	}
	buckets := map[time.Time]*IntervalPnL{}
	fees := map[time.Time]map[string]IntervalFee{}
	seen := map[string]string{}
	gasByTransaction := map[string]EconomicRecord{}
	for _, record := range records {
		leg := record.Leg
		id, err := leg.Identity.Key()
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		key := id + ":" + leg.Wallet + ":" + leg.Token
		if old, ok := seen[key]; ok {
			if old != string(raw) {
				return nil, delivery.ErrIntegrity
			}
			continue
		}
		seen[key] = string(raw)
		if record.GasPAXRaw != "" && record.GasPAXRaw != "0" && leg.Sender == leg.Wallet {
			if _, err = Uint256(record.GasPAXRaw); err != nil {
				return nil, err
			}
			transaction := leg.Identity.Chain + ":" + leg.Wallet + ":" + leg.Identity.Transaction
			if prior, ok := gasByTransaction[transaction]; ok {
				if prior.GasPAXRaw != record.GasPAXRaw || prior.BlockHash != record.BlockHash || prior.Leg.Height != leg.Height || prior.Leg.TransactionIndex != leg.TransactionIndex || !prior.CommittedAt.Equal(record.CommittedAt) {
					return nil, delivery.ErrIntegrity
				}
				record.GasPAXRaw = "0"
			} else {
				gasByTransaction[transaction] = record
			}
		}
		trade := leg.Kind == "buy" || leg.Kind == "sell"
		gasCharge := record.GasPAXRaw != "" && record.GasPAXRaw != "0" && leg.Sender == leg.Wallet
		if !trade && !gasCharge {
			continue
		}
		if err := leg.Validate(); err != nil {
			return nil, err
		}
		bucket := record.CommittedAt.UTC().Truncate(step)
		item := buckets[bucket]
		if item == nil {
			zero := "0"
			item = &IntervalPnL{Start: bucket, End: bucket.Add(step), GrossProceedsUsdlRaw: "0", KnownAllocatedBasisUsdlRaw: "0", KnownNetGainUsdlRaw: "0", UnknownProceedsUsdlRaw: "0", RealizedPnlUsdlRaw: &zero, Fees: []IntervalFee{}, GasPaxRaw: "0", AccountingComplete: true}
			buckets[bucket] = item
			fees[bucket] = map[string]IntervalFee{}
		}
		if trade {
			item.TradeCount++
		}
		if leg.Kind == "sell" {
			if record.Allocation == nil || leg.ActualUSDL == nil {
				return nil, delivery.ErrIntegrity
			}
			item.SaleCount++
			a := record.Allocation
			gross, e := Uint256(*leg.ActualUSDL)
			if e != nil {
				return nil, e
			}
			basis, e := aggregate(a.Cost, false)
			if e != nil {
				return nil, e
			}
			known, e := aggregate(a.KnownProceeds, false)
			if e != nil {
				return nil, e
			}
			unknown, e := aggregate(a.UnknownProceeds, false)
			if e != nil {
				return nil, e
			}
			unknownQuantity, e := Uint256(a.Unknown)
			if e != nil {
				return nil, e
			}
			knownQuantity, e := Uint256(a.Known)
			if e != nil {
				return nil, e
			}
			quantity, e := Uint256(leg.Quantity)
			if e != nil || add(knownQuantity, unknownQuantity).Cmp(quantity) != 0 {
				return nil, delivery.ErrIntegrity
			}
			if add(known, unknown).Cmp(gross) != 0 || ratio(gross, knownQuantity, quantity).Cmp(known) != 0 {
				return nil, delivery.ErrIntegrity
			}
			old, _ := aggregate(item.GrossProceedsUsdlRaw, false)
			item.GrossProceedsUsdlRaw = add(old, gross).String()
			old, _ = aggregate(item.KnownAllocatedBasisUsdlRaw, false)
			item.KnownAllocatedBasisUsdlRaw = add(old, basis).String()
			old, _ = aggregate(item.KnownNetGainUsdlRaw, true)
			item.KnownNetGainUsdlRaw = add(old, sub(known, basis)).String()
			old, _ = aggregate(item.UnknownProceedsUsdlRaw, false)
			item.UnknownProceedsUsdlRaw = add(old, unknown).String()
			item.AccountingComplete = item.AccountingComplete && unknownQuantity.Sign() == 0
		}
		if trade && record.FeeAsset != nil {
			value, e := Uint256(record.FeeRaw)
			if e != nil {
				return nil, e
			}
			chain, address, decimals := "125", record.FeeAsset.Address, int(record.FeeAsset.Decimals)
			key := chain + ":" + address + ":" + strconv.Itoa(decimals)
			fee := fees[bucket][key]
			if fee.AmountRaw == "" {
				fee = IntervalFee{Asset: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &address, Decimals: &decimals}, AmountRaw: "0"}
			}
			prior, _ := aggregate(fee.AmountRaw, false)
			fee.AmountRaw = add(prior, value).String()
			fees[bucket][key] = fee
		}
		if record.GasPAXRaw != "" && leg.Sender == leg.Wallet {
			gas, e := aggregate(record.GasPAXRaw, false)
			if e != nil {
				return nil, e
			}
			prior, _ := aggregate(item.GasPaxRaw, false)
			item.GasPaxRaw = add(prior, gas).String()
		}
	}
	out := make([]IntervalPnL, 0, len(buckets))
	for start, item := range buckets {
		keys := make([]string, 0, len(fees[start]))
		for key := range fees[start] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item.Fees = append(item.Fees, fees[start][key])
		}
		item.RealizedPnlUsdlRaw = nil
		if item.AccountingComplete {
			gain := item.KnownNetGainUsdlRaw
			item.RealizedPnlUsdlRaw = &gain
		}
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func (s *Service) commitRebuiltHistoryTx(ctx context.Context, tx pgx.Tx, wallet, snapshot, source string, snapshots []RebuildSnapshot, repository l17.Repository) error {
	records := []EconomicRecord{}
	start := snapshots[0].ObservedThrough
	end := start
	for _, replay := range snapshots {
		if len(replay.Checkpoints) == 0 || !replay.Checkpoints[0].CompleteHistory {
			return ErrBaselineUnavailable
		}
		day, e := time.Parse("2006-01-02", replay.Checkpoints[0].Day)
		if e != nil {
			return e
		}
		if day.Before(start) {
			start = day
		}
		if replay.ObservedThrough.After(end) {
			end = replay.ObservedThrough
		}
		partitionRecords, e := s.readReplayArchiveTx(ctx, tx, wallet, replay)
		if e != nil {
			return e
		}
		records = append(records, partitionRecords...)
		if len(records) > 1000000 {
			return ErrBaselineUnavailable
		}
	}
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i].Leg, records[j].Leg
		if a.Height != b.Height {
			return a.Height < b.Height
		}
		if a.TransactionIndex != b.TransactionIndex {
			return a.TransactionIndex < b.TransactionIndex
		}
		return a.Identity.Log < b.Identity.Log
	})
	gasByTransaction := map[string]EconomicRecord{}
	for i := range records {
		record := &records[i]
		if record.GasPAXRaw == "" || record.Leg.Sender != wallet {
			continue
		}
		gas, e := Uint256(record.GasPAXRaw)
		if e != nil {
			return e
		}
		if gas.Sign() == 0 {
			continue
		}
		transaction := record.Leg.Identity.Chain + ":" + wallet + ":" + record.Leg.Identity.Transaction
		if prior, ok := gasByTransaction[transaction]; ok {
			if prior.GasPAXRaw != record.GasPAXRaw || prior.BlockHash != record.BlockHash || prior.Leg.Height != record.Leg.Height || prior.Leg.TransactionIndex != record.Leg.TransactionIndex || !prior.CommittedAt.Equal(record.CommittedAt) {
				return delivery.ErrIntegrity
			}
			record.GasPAXRaw = "0"
		} else {
			gasByTransaction[transaction] = *record
		}
	}
	history := make([]l17.HistoryRecord, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		leg := record.Leg
		id, e := leg.Identity.Key()
		if e != nil {
			return e
		}
		if seen[id] {
			return delivery.ErrIntegrity
		}
		seen[id] = true
		chain, token := "125", leg.Token
		kind := "trade"
		quality := "complete"
		switch leg.Kind {
		case "incoming", "mint":
			kind = "transfer_in"
			quality = "unknown"
		case "outgoing", "burn":
			kind = "transfer_out"
			if record.Allocation == nil {
				return delivery.ErrIntegrity
			}
			if record.Allocation.Unknown != "0" {
				quality = "partial"
			}
		case "sell":
			if record.Allocation == nil {
				return delivery.ErrIntegrity
			}
			if record.Allocation.Unknown != "0" {
				quality = "partial"
			}
		}
		history = append(history, l17.HistoryRecord{EventID: id, Kind: kind, Asset: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &token, Decimals: record.TokenDecimals}, AmountRaw: leg.Quantity, QuoteAmountUsdlRaw: leg.ActualUSDL, BasisQuality: quality, Position: l17.ChainPosition{ChainID: "125", Height: strconv.FormatUint(leg.Height, 10), TransactionIndex: strconv.FormatUint(uint64(leg.TransactionIndex), 10), LogIndex: strconv.FormatUint(uint64(leg.Identity.Log), 10), TransactionHash: leg.Identity.Transaction, BlockHash: record.BlockHash}})
	}
	if err := repository.CommitHistoryTx(ctx, tx, wallet, snapshot, source, history); err != nil {
		return err
	}
	chain, currency, quoteAddress := "125", "USDL", "0x85FcD13735F4309833A503EE804ea32395851479"
	decimals := 6
	if start.Before(end) {
		if err := repository.CommitHistoryMetadataTx(ctx, tx, wallet, snapshot, l17.SnapshotHistoryMetadata{SourceEvent: source, Start: start, End: end, Quote: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &quoteAddress, Decimals: &decimals, CurrencyCode: &currency}, Periods: []l17.ReturnPeriod{}}); err != nil {
			return err
		}
	}
	var rawSummary []byte
	if err := tx.QueryRow(ctx, `SELECT summary FROM portfolio_snapshots WHERE id=$1 AND wallet=$2`, snapshot, wallet).Scan(&rawSummary); err != nil {
		return err
	}
	var summary l17.Summary
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		return err
	}
	watermark, e := json.Marshal(summary.Watermark)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(records)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO inventory_l16_wallet_ranges_v3 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, snapshot, wallet, s.authority.generation, start, end, s.release, delivery.Hash(raw), watermark); e != nil {
		return e
	}
	for _, interval := range []struct {
		name string
		step time.Duration
	}{{"1h", time.Hour}, {"1d", 24 * time.Hour}} {
		items, e := intervalRecords(records, interval.step)
		if e != nil {
			return e
		}
		values := make([][]any, len(items))
		for i, item := range items {
			raw, e := json.Marshal(item)
			if e != nil {
				return e
			}
			values[i] = []any{snapshot, interval.name, item.Start, raw}
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"inventory_l16_wallet_intervals_v3"}, []string{"snapshot_id", "interval", "bucket", "payload"}, pgx.CopyFromRows(values)); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) IntervalHistory(ctx context.Context, wallet string, from, to time.Time, interval string, limit int) (IntervalSeries, error) {
	out := IntervalSeries{Version: 3, Algorithm: Algorithm, Method: "canonical_interval_realized_v1", Wallet: wallet, Entries: []IntervalPnL{}}
	step := time.Hour
	if interval == "1d" {
		step = 24 * time.Hour
	} else if interval != "1h" {
		return out, l17.ErrInvalid
	}
	from = from.UTC()
	to = to.UTC()
	if !address.MatchString(wallet) || from.IsZero() || !from.Before(to) || !from.Equal(from.Truncate(step)) || !to.Equal(to.Truncate(step)) || limit < 1 || limit > 1000 || to.Sub(from) > time.Duration(limit)*step {
		return out, l17.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	tx, e := s.authority.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer inventoryRollback(tx)
	if _, e = tx.Exec(bounded, `SET LOCAL search_path TO `+s.authority.schema+`,pg_catalog`); e != nil {
		return out, e
	}
	if _, e = tx.Exec(bounded, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, e
	}
	var raw []byte
	var observed, start time.Time
	e = tx.QueryRow(bounded, `SELECT snapshot_id::text,start_at,end_at,release_digest,input_digest,watermark FROM inventory_l16_wallet_ranges_v3 WHERE wallet=$1 AND generation=$2 ORDER BY end_at DESC,snapshot_id DESC LIMIT 1`, wallet, s.authority.generation).Scan(&out.SnapshotID, &start, &observed, &out.ReleaseDigest, &out.InputDigest, &raw)
	if e != nil {
		return out, e
	}
	if from.Before(start) || to.After(observed) || out.ReleaseDigest != s.release {
		return out, ErrBaselineUnavailable
	}
	if e = json.Unmarshal(raw, &out.Watermark); e != nil {
		return out, e
	}
	sqlStep := "1 hour"
	if interval == "1d" {
		sqlStep = "1 day"
	}
	rows, e := tx.Query(bounded, `SELECT buckets.bucket,i.payload FROM generate_series($3::timestamptz,$4::timestamptz-interval '1 microsecond',$5::interval) AS buckets(bucket) LEFT JOIN inventory_l16_wallet_intervals_v3 i ON i.snapshot_id=$1 AND i.interval=$2 AND i.bucket=buckets.bucket ORDER BY buckets.bucket LIMIT $6`, out.SnapshotID, interval, from, to, sqlStep, limit)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var bucket time.Time
		var payload []byte
		if e = rows.Scan(&bucket, &payload); e != nil {
			rows.Close()
			return out, e
		}
		zero := "0"
		item := IntervalPnL{Start: bucket.UTC(), End: bucket.UTC().Add(step), GrossProceedsUsdlRaw: "0", KnownAllocatedBasisUsdlRaw: "0", KnownNetGainUsdlRaw: "0", UnknownProceedsUsdlRaw: "0", RealizedPnlUsdlRaw: &zero, Fees: []IntervalFee{}, GasPaxRaw: "0", AccountingComplete: true}
		if payload != nil {
			if e = json.Unmarshal(payload, &item); e != nil {
				rows.Close()
				return out, e
			}
			if !item.Start.Equal(bucket) || !item.End.Equal(bucket.Add(step)) {
				rows.Close()
				return out, delivery.ErrIntegrity
			}
		}
		out.Entries = append(out.Entries, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit(bounded)
}

func (s *Service) RebuildWallet(ctx context.Context, wallet string, head int64, repository l17.Repository) (l17.Summary, error) {
	if !address.MatchString(wallet) || wallet == zeroAddress || head <= 0 {
		return l17.Summary{}, ErrBaselineUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	rows, e := s.authority.pool.Query(bounded, `SELECT DISTINCT t.token FROM indexer.indexer_token_transfers_v1 t JOIN indexer.indexer_canonical_creations_v1 c ON c.token=t.token WHERE t.chain_id=125 AND t.height<=$1 AND (t.from_address=$2 OR t.to_address=$2) ORDER BY t.token`, head, wallet)
	if e != nil {
		return l17.Summary{}, e
	}
	tokens := []string{}
	for rows.Next() {
		var token string
		if e = rows.Scan(&token); e != nil {
			rows.Close()
			return l17.Summary{}, e
		}
		tokens = append(tokens, token)
		if len(tokens) > 100000 {
			rows.Close()
			return l17.Summary{}, ErrBaselineUnavailable
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return l17.Summary{}, e
	}
	for _, token := range tokens {
		active, err := s.Active(bounded, wallet, token)
		if err == nil && active.Height == head && active.Generation == s.authority.generation && active.ReleaseDigest == s.release {
			continue
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return l17.Summary{}, err
		}
		if _, err = s.RebuildArchive(bounded, wallet, token, head); err != nil {
			return l17.Summary{}, err
		}
	}
	return s.PublishRebuiltWallet(bounded, wallet, head, repository)
}

func replayEconomicRecord(prior Inventory, record EconomicRecord) (Inventory, error) {
	if err := record.Leg.Validate(); err != nil {
		return prior, err
	}
	switch record.Leg.Kind {
	case "sell", "outgoing", "burn":
		next, allocation, err := Dispose(prior, record.Leg.Quantity, record.Leg.ActualUSDL)
		if err != nil {
			return prior, err
		}
		if record.Allocation == nil || *record.Allocation != allocation || next != record.State {
			return prior, delivery.ErrIntegrity
		}
		return next, nil
	default:
		if record.Allocation != nil {
			return prior, delivery.ErrIntegrity
		}
		next, err := Apply(prior, record.Leg)
		if err != nil || next != record.State {
			return prior, delivery.ErrIntegrity
		}
		return next, nil
	}
}

func sameReplaySnapshot(a, b RebuildSnapshot) bool {
	a.SourceOwner, b.SourceOwner = "", ""
	a.SourceEpoch, b.SourceEpoch = 0, 0
	first, err := json.Marshal(a)
	if err != nil {
		return false
	}
	second, err := json.Marshal(b)
	return err == nil && string(first) == string(second)
}

func (s *Service) readReplayArchiveTx(ctx context.Context, tx pgx.Tx, wallet string, replay RebuildSnapshot) ([]EconomicRecord, error) {
	parts := strings.Split(replay.Partition, ":")
	if tx == nil || len(parts) != 3 || parts[0] != "125" || parts[1] != wallet || !address.MatchString(parts[2]) || replay.Height <= 0 {
		return nil, delivery.ErrIntegrity
	}
	rows, err := tx.Query(ctx, `SELECT payload,payload_hash FROM `+s.authority.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2 AND (convert_from(payload,'UTF8')::jsonb->'Leg'->>'Height')::numeric<=$3 ORDER BY (convert_from(payload,'UTF8')::jsonb->'Leg'->>'Height')::numeric,(convert_from(payload,'UTF8')::jsonb->'Leg'->>'TransactionIndex')::bigint,(convert_from(payload,'UTF8')::jsonb->'Leg'->'Identity'->>'Log')::bigint`, replay.Generation, replay.Partition, replay.Height)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]EconomicRecord, 0)
	last := ""
	state := Empty()
	var prior *Leg
	for rows.Next() {
		var raw []byte
		var digest string
		if err = rows.Scan(&raw, &digest); err != nil {
			return nil, err
		}
		if delivery.Hash(raw) != digest {
			return nil, delivery.ErrIntegrity
		}
		var record EconomicRecord
		if err = json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		leg := record.Leg
		if leg.Wallet != wallet || leg.Token != parts[2] || leg.Predecessor != last || leg.Height > uint64(replay.Height) || !hash.MatchString(record.BlockHash) || record.TokenDecimals == nil || *record.TokenDecimals < 0 || *record.TokenDecimals > 255 || record.CommittedAt.After(replay.ObservedThrough) {
			return nil, delivery.ErrIntegrity
		}
		if prior != nil && (leg.Height < prior.Height || leg.Height == prior.Height && (leg.TransactionIndex < prior.TransactionIndex || leg.TransactionIndex == prior.TransactionIndex && leg.Identity.Log <= prior.Identity.Log)) {
			return nil, delivery.ErrIntegrity
		}
		state, err = replayEconomicRecord(state, record)
		if err != nil {
			return nil, err
		}
		last, err = leg.Identity.Key()
		if err != nil {
			return nil, err
		}
		prior = &leg
		records = append(records, record)
		if len(records) > 1000000 {
			return nil, ErrBaselineUnavailable
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return nil, err
	}
	if last != replay.LastIdentity || state != replay.State || delivery.Hash(raw) != replay.InputDigest {
		return nil, delivery.ErrIntegrity
	}
	return records, nil
}
