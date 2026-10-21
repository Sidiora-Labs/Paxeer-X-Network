package l16

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
var balanceSelector = crypto.Keccak256([]byte("balanceOf(address)"))[:4]
var totalSupplySelector = crypto.Keccak256([]byte("totalSupply()"))[:4]

const BaselineDDL = `CREATE TABLE inventory_l16_baselines (
 baseline_digest text PRIMARY KEY CHECK(baseline_digest ~ '^[0-9a-f]{64}$'), consumer_id text NOT NULL,
 projection_generation text NOT NULL, partition text NOT NULL, wallet text NOT NULL, token text NOT NULL,
 creation_height bigint NOT NULL CHECK(creation_height>0), creation_block_hash text NOT NULL,
 source_stream text NOT NULL, source_owner text NOT NULL, source_epoch bigint NOT NULL CHECK(source_epoch>0),
 movement_digest text NOT NULL, observed_balance_raw text NOT NULL CHECK(observed_balance_raw='0'),
 attestation bytea NOT NULL, captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(consumer_id,projection_generation,partition,source_stream,source_owner,source_epoch)
);
CREATE VIEW inventory_l16_baselines_v1 AS SELECT baseline_digest,consumer_id,projection_generation,partition,wallet,token,creation_height,creation_block_hash,source_stream,source_owner,source_epoch,movement_digest,observed_balance_raw FROM inventory_l16_baselines;
CREATE TABLE inventory_l16_baseline_uses (
 projection_generation text NOT NULL, partition text NOT NULL, effect_identity text NOT NULL,
 owner_epoch bigint NOT NULL CHECK(owner_epoch>0), baseline_digest text NOT NULL REFERENCES inventory_l16_baselines,
 checkpoint_height bigint NOT NULL, checkpoint_hash text NOT NULL, movement_digest text NOT NULL,
 replayed_balance_raw text NOT NULL, attestation_digest text NOT NULL, attestation bytea NOT NULL,
 PRIMARY KEY(projection_generation,partition,effect_identity,owner_epoch)
);`

type BaselineAuthority struct {
	pool                                                                   *pgxpool.Pool
	rpc                                                                    *ethclient.Client
	schema, sourceStream, consumerID, generation, owner, projectionVersion string
	// factory is the deployed SidioraFactory (shared/config/deployment
	// manifest role), the only minter of a canonical market's supply.
	factory    string
	epochGuard func(context.Context, pgx.Tx) error
}

type baselineAttestation struct {
	ConsumerID, Generation, Partition, Wallet, Token   string
	CreationHeight                                     int64
	CreationBlockHash, Pool, SourceStream, SourceOwner string
	SourceEpoch                                        int64
	MovementDigest, ObservedBalanceRaw, CodeSHA256     string
}

type movement struct {
	TransactionHash             string
	LogIndex                    uint32
	Height                      int64
	TransactionIndex            uint32
	BlockHash, From, To, Amount string
}

type baselineUse struct {
	BaselineDigest, Generation, Partition, EffectIdentity string
	OwnerEpoch, CheckpointHeight                          int64
	CheckpointHash, MovementDigest, ReplayedBalanceRaw    string
}

func NewBaselineAuthority(pool *pgxpool.Pool, rpc *ethclient.Client, schema, sourceStream, consumerID, generation, owner, projectionVersion string) (*BaselineAuthority, error) {
	if pool == nil || rpc == nil || !regexpToken(schema) || !regexpToken(sourceStream) || !regexpToken(consumerID) || !regexpToken(generation) || !regexpToken(owner) || projectionVersion == "" || len(projectionVersion) > 128 {
		return nil, ErrBaselineUnavailable
	}
	role, ok := deployment.Current().Roles["SidioraFactory"]
	factory := strings.ToLower(role.Address)
	if !ok || !address.MatchString(factory) || factory == zeroAddress {
		return nil, ErrBaselineUnavailable
	}
	return &BaselineAuthority{pool: pool, rpc: rpc, schema: pgx.Identifier{schema}.Sanitize(), sourceStream: sourceStream, consumerID: consumerID, generation: generation, owner: owner, projectionVersion: projectionVersion, factory: factory}, nil
}

func NewBaselineAuthorityWithEpochGuard(pool *pgxpool.Pool, rpc *ethclient.Client, schema, sourceStream, consumerID, generation, owner, projectionVersion string, guard func(context.Context, pgx.Tx) error) (*BaselineAuthority, error) {
	if guard == nil {
		return nil, errors.New("baseline recovery epoch guard required")
	}
	a, err := NewBaselineAuthority(pool, rpc, schema, sourceStream, consumerID, generation, owner, projectionVersion)
	if err != nil {
		return nil, err
	}
	a.epochGuard = guard
	return a, nil
}

func (a *BaselineAuthority) table(name string) string { return a.schema + "." + name }

func (a *BaselineAuthority) sourceLease(ctx context.Context, tx pgx.Tx) (string, int64, error) {
	var owner string
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT owner_instance,fence_token FROM indexer.indexer_ingest_leases_v1 WHERE chain_id=125 AND stream_id=$1 AND expires_at>clock_timestamp()`, a.sourceStream).Scan(&owner, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, delivery.ErrLease
	}
	return owner, epoch, err
}

func decodeTransfer(log types.Log) (movement, error) {
	var m movement
	if log.Removed || len(log.Topics) != 3 || log.Topics[0] != transferTopic || len(log.Data) != 32 || log.BlockNumber > 1<<63-1 || uint64(log.TxIndex) > 1<<32-1 || uint64(log.Index) > 1<<32-1 {
		return m, delivery.ErrIntegrity
	}
	if !bytes.Equal(log.Topics[1].Bytes()[:12], make([]byte, 12)) || !bytes.Equal(log.Topics[2].Bytes()[:12], make([]byte, 12)) {
		return m, delivery.ErrIntegrity
	}
	return movement{TransactionHash: strings.ToLower(log.TxHash.Hex()), LogIndex: uint32(log.Index), Height: int64(log.BlockNumber), TransactionIndex: uint32(log.TxIndex), BlockHash: strings.ToLower(log.BlockHash.Hex()), From: strings.ToLower(common.BytesToAddress(log.Topics[1].Bytes()[12:]).Hex()), To: strings.ToLower(common.BytesToAddress(log.Topics[2].Bytes()[12:]).Hex()), Amount: new(big.Int).SetBytes(log.Data).String()}, nil
}

func (a *BaselineAuthority) coverage(ctx context.Context, tx pgx.Tx, start, end int64) (map[int64]string, error) {
	if start <= 0 || end < start || end-start > 10000 {
		return nil, ErrBaselineUnavailable
	}
	var watermark int64
	if err := tx.QueryRow(ctx, `SELECT LEAST(raw_complete_height,typed_complete_height) FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1`, a.sourceStream).Scan(&watermark); err != nil {
		return nil, err
	}
	if watermark < end {
		return nil, delivery.ErrGap
	}
	rows, err := tx.Query(ctx, `SELECT c.height,c.block_hash,c.raw_count,(SELECT count(*) FROM indexer.indexer_raw_logs_v1 r WHERE r.chain_id=125 AND r.height=c.height) FROM indexer.indexer_block_completeness_v1 c WHERE c.chain_id=125 AND c.stream_id=$1 AND c.height BETWEEN $2 AND $3 AND c.fetch_complete AND c.required_decode_complete ORDER BY c.height`, a.sourceStream, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blocks := map[int64]string{}
	next := start
	for rows.Next() {
		var h, expected, actual int64
		var hash string
		if err = rows.Scan(&h, &hash, &expected, &actual); err != nil {
			return nil, err
		}
		if h != next || expected != actual {
			return nil, delivery.ErrGap
		}
		blocks[h] = hash
		next++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if next != end+1 {
		return nil, delivery.ErrGap
	}
	return blocks, nil
}

func (a *BaselineAuthority) verifiedMovements(ctx context.Context, tx pgx.Tx, token string, start, end int64) ([]movement, string, string, error) {
	blocks, err := a.coverage(ctx, tx, start, end)
	if err != nil {
		return nil, "", "", err
	}
	chain, err := a.rpc.ChainID(ctx)
	if err != nil {
		return nil, "", "", err
	}
	if chain.Cmp(big.NewInt(125)) != 0 {
		return nil, "", "", delivery.ErrIntegrity
	}
	_, reportedEnd, err := reportedHeaderByNumber(ctx, a.rpc, big.NewInt(end))
	if err != nil {
		return nil, "", "", err
	}
	if reportedEnd != blocks[end] {
		return nil, "", "", delivery.ErrIntegrity
	}
	logs, err := a.rpc.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: big.NewInt(start), ToBlock: big.NewInt(end), Addresses: []common.Address{common.HexToAddress(token)}, Topics: [][]common.Hash{{transferTopic}}})
	if err != nil {
		return nil, "", "", err
	}
	if len(logs) > 100000 {
		return nil, "", "", ErrBaselineUnavailable
	}
	known := map[string]movement{}
	rows, err := tx.Query(ctx, `SELECT t.transaction_hash,t.log_index,t.height,t.transaction_index,t.block_hash,t.from_address,t.to_address,t.amount_raw,r.emitter,r.topics,r.data FROM indexer.indexer_token_transfers_v1 t JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=t.chain_id AND r.transaction_hash=t.transaction_hash AND r.log_index=t.log_index WHERE t.chain_id=125 AND t.token=$1 AND t.height BETWEEN $2 AND $3 ORDER BY t.height,t.transaction_index,t.log_index`, token, start, end)
	if err != nil {
		return nil, "", "", err
	}
	for rows.Next() {
		var m movement
		var logIndex, txIndex int64
		var emitter string
		var topicsRaw, data []byte
		if err = rows.Scan(&m.TransactionHash, &logIndex, &m.Height, &txIndex, &m.BlockHash, &m.From, &m.To, &m.Amount, &emitter, &topicsRaw, &data); err != nil {
			rows.Close()
			return nil, "", "", err
		}
		if logIndex < 0 || logIndex > 1<<32-1 || txIndex < 0 || txIndex > 1<<32-1 || emitter != token {
			rows.Close()
			return nil, "", "", delivery.ErrIntegrity
		}
		m.LogIndex = uint32(logIndex)
		m.TransactionIndex = uint32(txIndex)
		var topics []string
		if err = json.Unmarshal(topicsRaw, &topics); err != nil {
			rows.Close()
			return nil, "", "", err
		}
		if len(topics) != 3 {
			rows.Close()
			return nil, "", "", delivery.ErrIntegrity
		}
		reconstructed := types.Log{Address: common.HexToAddress(emitter), Topics: []common.Hash{common.HexToHash(topics[0]), common.HexToHash(topics[1]), common.HexToHash(topics[2])}, Data: data, BlockNumber: uint64(m.Height), TxHash: common.HexToHash(m.TransactionHash), TxIndex: uint(m.TransactionIndex), BlockHash: common.HexToHash(m.BlockHash), Index: uint(m.LogIndex)}
		decoded, decodeErr := decodeTransfer(reconstructed)
		if decodeErr != nil || decoded != m || blocks[m.Height] != m.BlockHash {
			rows.Close()
			return nil, "", "", delivery.ErrIntegrity
		}
		key := m.TransactionHash + ":" + strconv.FormatUint(uint64(m.LogIndex), 10)
		if _, exists := known[key]; exists {
			rows.Close()
			return nil, "", "", delivery.ErrIntegrity
		}
		known[key] = m
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", "", err
	}
	if len(known) != len(logs) {
		return nil, "", "", delivery.ErrGap
	}
	result := make([]movement, 0, len(logs))
	seen := map[string]bool{}
	for _, log := range logs {
		m, decodeErr := decodeTransfer(log)
		if decodeErr != nil || strings.ToLower(log.Address.Hex()) != token || blocks[m.Height] != m.BlockHash {
			return nil, "", "", delivery.ErrIntegrity
		}
		key := m.TransactionHash + ":" + strconv.FormatUint(uint64(m.LogIndex), 10)
		if seen[key] || known[key] != m {
			return nil, "", "", delivery.ErrIntegrity
		}
		seen[key] = true
		result = append(result, m)
	}
	for i := 1; i < len(result); i++ {
		x, y := result[i-1], result[i]
		if y.Height < x.Height || y.Height == x.Height && (y.TransactionIndex < x.TransactionIndex || y.TransactionIndex == x.TransactionIndex && y.LogIndex <= x.LogIndex) {
			return nil, "", "", delivery.ErrIntegrity
		}
	}
	_, reportedAfter, err := reportedHeaderByNumber(ctx, a.rpc, big.NewInt(end))
	if err != nil {
		return nil, "", "", err
	}
	if reportedAfter != reportedEnd {
		return nil, "", "", delivery.ErrIntegrity
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, "", "", err
	}
	return result, delivery.Hash(raw), blocks[end], nil
}

func (a *BaselineAuthority) observedBalance(ctx context.Context, token, wallet, blockHash string) (*big.Int, error) {
	data := append(append([]byte(nil), balanceSelector...), common.LeftPadBytes(common.HexToAddress(wallet).Bytes(), 32)...)
	result, err := a.rpc.CallContractAtHash(ctx, ethereum.CallMsg{To: func() *common.Address { v := common.HexToAddress(token); return &v }(), Data: data}, common.HexToHash(blockHash))
	if err != nil {
		return nil, err
	}
	if len(result) != 32 {
		return nil, delivery.ErrIntegrity
	}
	return new(big.Int).SetBytes(result), nil
}

func (a *BaselineAuthority) observedTotalSupply(ctx context.Context, token, blockHash string) (*big.Int, error) {
	to := common.HexToAddress(token)
	result, err := a.rpc.CallContractAtHash(ctx, ethereum.CallMsg{To: &to, Data: append([]byte(nil), totalSupplySelector...)}, common.HexToHash(blockHash))
	if err != nil {
		return nil, err
	}
	if len(result) != 32 {
		return nil, delivery.ErrIntegrity
	}
	return new(big.Int).SetBytes(result), nil
}

// factoryCreationSupply proves the deployed creation path of a canonical
// market at its creation block and returns the minted supply. The deployed
// SidioraFactory._createMarket (contracts/core/SidioraFactory.sol) CREATE2s
// SidioraERC20, whose constructor mints the whole supply once to its recipient,
// the factory (contracts/core/SidioraERC20.sol, no mint/burn afterwards), binds
// the pool, then safeTransfers exactly that virtual token supply to the pool.
// On chain 125 (market 0xce20…6263, block 33963716, transaction
// 0xe200486f…cbfca) the token emits exactly two Transfer logs in the creation
// transaction: 0x0 -> factory (log 2) and factory -> pool (log 10), both
// 1000000000000000. Anything else in the creation transaction — a mint to any
// other holder, a partial or extra transfer, a transfer before the mint — is
// not this creation path and has no fresh baseline.
func factoryCreationSupply(movements []movement, creationTx, factory, pool string) (*big.Int, error) {
	if !address.MatchString(factory) || !address.MatchString(pool) || factory == pool || !partitionHash(strings.TrimPrefix(creationTx, "0x")) || !strings.HasPrefix(creationTx, "0x") {
		return nil, ErrBaselineUnavailable
	}
	creation := make([]movement, 0, 2)
	for _, m := range movements {
		if m.TransactionHash == creationTx {
			creation = append(creation, m)
		}
	}
	if len(creation) != 2 {
		return nil, ErrBaselineUnavailable
	}
	mint, seed := creation[0], creation[1]
	if mint.From != zeroAddress || mint.To != factory || seed.From != factory || seed.To != pool || mint.Amount != seed.Amount || mint.LogIndex >= seed.LogIndex {
		return nil, ErrBaselineUnavailable
	}
	supply, err := accounting.Uint(mint.Amount)
	if err != nil || supply.Sign() <= 0 {
		return nil, ErrBaselineUnavailable
	}
	for _, m := range movements {
		earlier := m.TransactionIndex < mint.TransactionIndex || m.TransactionIndex == mint.TransactionIndex && m.LogIndex < mint.LogIndex
		if earlier {
			return nil, ErrBaselineUnavailable
		}
	}
	return supply, nil
}

func (a *BaselineAuthority) Capture(ctx context.Context, wallet, token string) (accounting.Digest, error) {
	if !address.MatchString(wallet) || !address.MatchString(token) || wallet == "0x"+strings.Repeat("0", 40) || token == "0x"+strings.Repeat("0", 40) {
		return "", ErrBaselineUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tx, err := a.pool.Begin(bounded)
	if err != nil {
		return "", err
	}
	defer inventoryRollback(tx)
	if a.epochGuard != nil {
		if err = a.epochGuard(bounded, tx); err != nil {
			return "", err
		}
	}
	var height int64
	var blockHash, pool, creationTx string
	if err = tx.QueryRow(bounded, `SELECT height,block_hash,pool,transaction_hash FROM indexer.indexer_canonical_creations_v1 WHERE token=$1`, token).Scan(&height, &blockHash, &pool, &creationTx); err != nil {
		return "", err
	}
	owner, epoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return "", err
	}
	movements, movementDigest, checkpointHash, err := a.verifiedMovements(bounded, tx, token, height, height)
	if err != nil {
		return "", err
	}
	if checkpointHash != blockHash {
		return "", delivery.ErrIntegrity
	}
	for _, m := range movements {
		if m.From == wallet || m.To == wallet {
			return "", ErrBaselineUnavailable
		}
	}
	supply, err := factoryCreationSupply(movements, creationTx, a.factory, pool)
	if err != nil {
		return "", err
	}
	before, err := a.rpc.CodeAt(bounded, common.HexToAddress(token), big.NewInt(height-1))
	if err != nil {
		return "", err
	}
	if len(before) != 0 {
		return "", ErrBaselineUnavailable
	}
	code, err := a.rpc.CodeAtHash(bounded, common.HexToAddress(token), common.HexToHash(blockHash))
	if err != nil {
		return "", err
	}
	if len(code) == 0 {
		return "", ErrBaselineUnavailable
	}
	total, err := a.observedTotalSupply(bounded, token, blockHash)
	if err != nil {
		return "", err
	}
	if total.Cmp(supply) != 0 {
		return "", ErrBaselineUnavailable
	}
	balance, err := a.observedBalance(bounded, token, wallet, blockHash)
	if err != nil {
		return "", err
	}
	if balance.Sign() != 0 {
		return "", ErrBaselineUnavailable
	}
	att := baselineAttestation{ConsumerID: a.consumerID, Generation: a.generation, Partition: "125:" + wallet + ":" + token, Wallet: wallet, Token: token, CreationHeight: height, CreationBlockHash: blockHash, Pool: pool, SourceStream: a.sourceStream, SourceOwner: owner, SourceEpoch: epoch, MovementDigest: movementDigest, ObservedBalanceRaw: "0", CodeSHA256: delivery.Hash(code)}
	raw, err := json.Marshal(att)
	if err != nil {
		return "", err
	}
	digest := accounting.Digest(delivery.Hash(raw))
	if _, err = tx.Exec(bounded, "INSERT INTO "+a.table("inventory_l16_baselines")+`(baseline_digest,consumer_id,projection_generation,partition,wallet,token,creation_height,creation_block_hash,source_stream,source_owner,source_epoch,movement_digest,observed_balance_raw,attestation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'0',$13) ON CONFLICT DO NOTHING`, string(digest), a.consumerID, a.generation, att.Partition, wallet, token, height, blockHash, a.sourceStream, owner, epoch, movementDigest, raw); err != nil {
		return "", err
	}
	var stored []byte
	if err = tx.QueryRow(bounded, "SELECT attestation FROM "+a.table("inventory_l16_baselines")+" WHERE baseline_digest=$1", string(digest)).Scan(&stored); err != nil {
		return "", err
	}
	if !bytes.Equal(stored, raw) {
		return "", delivery.ErrIntegrity
	}
	finalOwner, finalEpoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return "", err
	}
	if finalOwner != owner || finalEpoch != epoch {
		return "", delivery.ErrLease
	}
	if a.epochGuard != nil {
		if err = a.epochGuard(bounded, tx); err != nil {
			return "", err
		}
	}
	return digest, tx.Commit(bounded)
}

func (a *BaselineAuthority) readBaseline(ctx context.Context, tx pgx.Tx, partition string) (baselineAttestation, string, error) {
	var att baselineAttestation
	var raw []byte
	var digest string
	owner, epoch, err := a.sourceLease(ctx, tx)
	if err != nil {
		return att, "", err
	}
	err = tx.QueryRow(ctx, "SELECT baseline_digest,attestation FROM "+a.table("inventory_l16_baselines")+` WHERE consumer_id=$1 AND projection_generation=$2 AND partition=$3 AND source_stream=$4 AND source_owner=$5 AND source_epoch=$6`, a.consumerID, a.generation, partition, a.sourceStream, owner, epoch).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return att, "", ErrBaselineUnavailable
	}
	if err != nil {
		return att, "", err
	}
	if err = json.Unmarshal(raw, &att); err != nil {
		return att, "", delivery.ErrIntegrity
	}
	if digest != delivery.Hash(raw) || att.ConsumerID != a.consumerID || att.Generation != a.generation || att.Partition != partition || att.SourceStream != a.sourceStream || att.SourceOwner != owner || att.SourceEpoch != epoch || att.ObservedBalanceRaw != "0" {
		return att, "", delivery.ErrIntegrity
	}
	return att, digest, nil
}

// issuedBaseline is one baseline row of a partition, decoded and checked.
type issuedBaseline struct {
	att    baselineAttestation
	digest string
}

// maxNamedBaselines bounds the past-lease baselines a frame may name.
const maxNamedBaselines = 256

// readNamedBaselines returns the current source lease and every baseline this
// authority issued for partition under a source epoch not beyond the current
// fence (newest first, bounded). Each row must hash to its digest and its
// attestation must name the row's own lease, consumer, generation, partition
// and stream, and all rows must attest the same wallet, token, pool and
// creation block: they differ only in the lease they were issued under.
func (a *BaselineAuthority) readNamedBaselines(ctx context.Context, tx pgx.Tx, partition string) ([]issuedBaseline, string, int64, error) {
	owner, epoch, err := a.sourceLease(ctx, tx)
	if err != nil {
		return nil, "", 0, err
	}
	rows, err := tx.Query(ctx, "SELECT baseline_digest,source_owner,source_epoch,attestation FROM "+a.table("inventory_l16_baselines")+` WHERE consumer_id=$1 AND projection_generation=$2 AND partition=$3 AND source_stream=$4 AND source_epoch<=$5 ORDER BY source_epoch DESC,source_owner LIMIT $6`, a.consumerID, a.generation, partition, a.sourceStream, epoch, maxNamedBaselines)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()
	out := []issuedBaseline{}
	for rows.Next() {
		var b issuedBaseline
		var rowOwner string
		var rowEpoch int64
		var raw []byte
		if err = rows.Scan(&b.digest, &rowOwner, &rowEpoch, &raw); err != nil {
			return nil, "", 0, err
		}
		if err = json.Unmarshal(raw, &b.att); err != nil {
			return nil, "", 0, delivery.ErrIntegrity
		}
		if b.digest != delivery.Hash(raw) || b.att.ConsumerID != a.consumerID || b.att.Generation != a.generation || b.att.Partition != partition || b.att.SourceStream != a.sourceStream || b.att.SourceOwner != rowOwner || b.att.SourceEpoch != rowEpoch || b.att.ObservedBalanceRaw != "0" {
			return nil, "", 0, delivery.ErrIntegrity
		}
		if len(out) > 0 {
			first := out[0].att
			if b.att.Wallet != first.Wallet || b.att.Token != first.Token || b.att.Pool != first.Pool || b.att.CreationHeight != first.CreationHeight || b.att.CreationBlockHash != first.CreationBlockHash {
				return nil, "", 0, delivery.ErrIntegrity
			}
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, "", 0, err
	}
	if len(out) == 0 {
		return nil, "", 0, ErrBaselineUnavailable
	}
	return out, owner, epoch, nil
}

func (a *BaselineAuthority) PreparePartition(ctx context.Context, partition string) (delivery.BootstrapInput, error) {
	var in delivery.BootstrapInput
	parts := strings.Split(partition, ":")
	if len(parts) != 3 || parts[0] != "125" {
		return in, ErrBaselineUnavailable
	}
	digest, err := a.Capture(ctx, parts[1], parts[2])
	if err != nil {
		return in, err
	}
	store, err := delivery.NewPartitionStore(a.pool, strings.Trim(a.schema, `"`))
	if err != nil {
		return in, err
	}
	lease, err := store.Acquire(ctx, delivery.ConsumerKey{ConsumerID: a.consumerID, ProjectionGeneration: a.generation, PartitionKind: "inventory", PartitionKey: partition}, a.owner, 30*time.Second)
	if err != nil {
		return in, err
	}
	return delivery.BootstrapInput{Lease: lease, CheckpointDigest: digest, NextSequence: "1", PreviousEffectIdentity: ""}, nil
}

func (a *BaselineAuthority) VerifyBootstrapTx(ctx context.Context, tx pgx.Tx, in delivery.BootstrapInput) error {
	if tx == nil || in.Lease.Key.ConsumerID != a.consumerID || in.Lease.Key.ProjectionGeneration != a.generation || in.Lease.Key.PartitionKind != "inventory" || in.Lease.Owner != a.owner || in.NextSequence != "1" || in.PreviousEffectIdentity != "" {
		return delivery.ErrIntegrity
	}
	_, digest, err := a.readBaseline(ctx, tx, in.Lease.Key.PartitionKey)
	if err != nil {
		return err
	}
	if digest != string(in.CheckpointDigest) {
		return delivery.ErrIntegrity
	}
	return nil
}

type qualifiedBaselineUse struct {
	issuer              *BaselineAuthority
	tx                  pgx.Tx
	value               baselineUse
	raw                 []byte
	digest, sourceOwner string
	sourceEpoch         int64
}

func (a *BaselineAuthority) VerifyFreshSameOwnerTx(ctx context.Context, tx pgx.Tx, generation string, fill accounting.FillV1, p accounting.PartitionPosition) error {
	return a.qualifyFreshTx(ctx, tx, generation, fill, p, nil)
}

func (a *BaselineAuthority) qualifyFreshTx(ctx context.Context, tx pgx.Tx, generation string, fill accounting.FillV1, p accounting.PartitionPosition, qualified *qualifiedBaselineUse) error {
	if tx == nil || generation != a.generation || accounting.ValidateFillV1(fill) != nil || accounting.ValidatePartitionPosition(p) != nil || p.ProjectionVersion != a.projectionVersion || p.PartitionKind != "inventory" || p.SourceEventID != fill.SourceFillID || p.SourceOrder != fill.Position {
		return delivery.ErrIntegrity
	}
	// Baseline binding across indexer fence changes: the indexer builds a
	// frame's evidence against the baseline issued for the source lease
	// (owner, fence) of the canonical commit that published it (core/indexer
	// processor creation.go), and the evidence digest commits to that
	// baseline's digest, whose attestation names the lease. The frame is
	// verified against the baseline it names: the issued row whose digest
	// reproduces the published evidence, among the rows issued under a source
	// epoch not beyond the current fence. A frame still in flight when the
	// fence moves stays verifiable; a frame naming no such row is refused.
	named, leaseOwner, leaseEpoch, err := a.readNamedBaselines(ctx, tx, p.PartitionKey)
	if err != nil {
		return err
	}
	att := named[0].att
	if fill.Token.Address != att.Token || fill.PoolAddress != att.Pool {
		return ErrCostAllocationUnavailable
	}
	var publishedRaw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM indexer.indexer_accounting_publications_v1 WHERE payload->>'facet'='inventory' AND payload->'positions'->0->>'effectIdentity'=$1`, p.EffectIdentity).Scan(&publishedRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.ErrGap
	}
	if err != nil {
		return err
	}
	published, err := accounting.DecodePublicationV1(publishedRaw)
	if err != nil {
		return delivery.ErrIntegrity
	}
	sourceDigest, err := accounting.FillDigest(fill)
	if err != nil || published.Facet != "inventory" || published.SourceFillID != fill.SourceFillID || published.SourceFactDigest != sourceDigest || published.Positions[0] != p {
		return delivery.ErrIntegrity
	}
	if _, err = InventoryLeg(fill, published); err != nil {
		return err
	}
	end, err := strconv.ParseInt(fill.Position.Height, 10, 64)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	movements, movementDigest, checkpointHash, err := a.verifiedMovements(bounded, tx, att.Token, att.CreationHeight, end)
	if err != nil {
		return err
	}
	fills := []accounting.FillV1{}
	rows, err := tx.Query(bounded, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE (payload->'position'->>'height')::bigint BETWEEN $1 AND $2 AND payload->'token'->>'address'=$3 ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint`, att.CreationHeight, end, att.Token)
	if err != nil {
		return err
	}
	currentFound := false
	for rows.Next() {
		var raw []byte
		var expected string
		if err = rows.Scan(&raw, &expected); err != nil {
			rows.Close()
			return err
		}
		f, decodeErr := accounting.DecodeFillV1(raw)
		if decodeErr != nil {
			rows.Close()
			return delivery.ErrIntegrity
		}
		digest, digestErr := accounting.FillDigest(f)
		if digestErr != nil || string(digest) != expected {
			rows.Close()
			return delivery.ErrIntegrity
		}
		if f.SourceFillID == fill.SourceFillID {
			supplied, _ := accounting.FillDigest(fill)
			if digest != supplied {
				rows.Close()
				return delivery.ErrIntegrity
			}
			currentFound = true
		}
		fills = append(fills, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !currentFound {
		return delivery.ErrGap
	}
	balance, err := replaySameOwnerMovements(att.Wallet, att.Token, att.Pool, movements, fills)
	if err != nil {
		return err
	}
	payloadRows, err := tx.Query(bounded, `SELECT payload FROM indexer.indexer_token_transfers_v1 WHERE token=$1 AND height BETWEEN $2 AND $3 AND (from_address=$4 OR to_address=$4) ORDER BY height,transaction_index,log_index`, att.Token, att.CreationHeight, end, att.Wallet)
	if err != nil {
		return err
	}
	movementPayload := make([]json.RawMessage, 0)
	for payloadRows.Next() {
		var raw []byte
		if err = payloadRows.Scan(&raw); err != nil {
			payloadRows.Close()
			return err
		}
		movementPayload = append(movementPayload, append(json.RawMessage(nil), raw...))
	}
	err = payloadRows.Err()
	payloadRows.Close()
	if err != nil {
		return err
	}
	movementBytes, err := json.Marshal(movementPayload)
	if err != nil {
		return err
	}
	if published.Enrichment == nil {
		return delivery.ErrIntegrity
	}
	baselineDigest := ""
	for _, candidate := range named {
		if published.Enrichment.EvidenceDigest == accounting.Digest(accounting.HashTuple("fresh-inventory-evidence-v1", candidate.digest, candidate.att.MovementDigest, string(sourceDigest), string(movementBytes))) {
			att, baselineDigest = candidate.att, candidate.digest
			break
		}
	}
	if baselineDigest == "" {
		return delivery.ErrIntegrity
	}
	observed, err := a.observedBalance(bounded, att.Token, att.Wallet, checkpointHash)
	if err != nil {
		return err
	}
	if observed.Cmp(balance) != 0 {
		return delivery.ErrIntegrity
	}
	var epoch int64
	err = tx.QueryRow(bounded, "SELECT epoch FROM "+a.table("delivery_partition_leases")+` WHERE consumer_id=$1 AND projection_generation=$2 AND partition_kind='inventory' AND partition_key=$3 AND owner=$4 AND expires_at>clock_timestamp() FOR UPDATE`, a.consumerID, a.generation, p.PartitionKey, a.owner).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.ErrLease
	}
	if err != nil {
		return err
	}
	use := baselineUse{BaselineDigest: baselineDigest, Generation: generation, Partition: p.PartitionKey, EffectIdentity: p.EffectIdentity, OwnerEpoch: epoch, CheckpointHeight: end, CheckpointHash: checkpointHash, MovementDigest: movementDigest, ReplayedBalanceRaw: balance.String()}
	raw, err := json.Marshal(use)
	if err != nil {
		return err
	}
	digest := delivery.Hash(raw)
	if qualified != nil {
		*qualified = qualifiedBaselineUse{issuer: a, tx: tx, value: use, raw: append([]byte(nil), raw...), digest: digest, sourceOwner: leaseOwner, sourceEpoch: leaseEpoch}
	}
	// The source lease the named baselines were read under must still hold.
	finalOwner, finalEpoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return err
	}
	if finalOwner != leaseOwner || finalEpoch != leaseEpoch {
		return delivery.ErrLease
	}
	return nil
}

func (a *BaselineAuthority) persistFreshUseTx(ctx context.Context, tx pgx.Tx, q qualifiedBaselineUse) error {
	if q.issuer != a || q.tx != tx || q.value.Generation != a.generation || q.digest != delivery.Hash(q.raw) {
		return delivery.ErrIntegrity
	}
	var decoded baselineUse
	if err := json.Unmarshal(q.raw, &decoded); err != nil || decoded != q.value {
		return delivery.ErrIntegrity
	}
	owner, epoch, err := a.sourceLease(ctx, tx)
	if err != nil {
		return err
	}
	if owner != q.sourceOwner || epoch != q.sourceEpoch {
		return delivery.ErrLease
	}
	u := q.value
	if _, err = tx.Exec(ctx, "INSERT INTO "+a.table("inventory_l16_baseline_uses")+`(projection_generation,partition,effect_identity,owner_epoch,baseline_digest,checkpoint_height,checkpoint_hash,movement_digest,replayed_balance_raw,attestation_digest,attestation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, u.Generation, u.Partition, u.EffectIdentity, u.OwnerEpoch, u.BaselineDigest, u.CheckpointHeight, u.CheckpointHash, u.MovementDigest, u.ReplayedBalanceRaw, q.digest, q.raw); err != nil {
		return err
	}
	var old []byte
	var oldDigest string
	if err = tx.QueryRow(ctx, "SELECT attestation,attestation_digest FROM "+a.table("inventory_l16_baseline_uses")+` WHERE projection_generation=$1 AND partition=$2 AND effect_identity=$3 AND owner_epoch=$4`, u.Generation, u.Partition, u.EffectIdentity, u.OwnerEpoch).Scan(&old, &oldDigest); err != nil {
		return err
	}
	if !bytes.Equal(old, q.raw) || oldDigest != q.digest {
		return delivery.ErrIntegrity
	}
	return nil
}

func replaySameOwnerMovements(wallet, token, pool string, movements []movement, fills []accounting.FillV1) (*big.Int, error) {
	balance := new(big.Int)
	used := map[string]bool{}
	for _, m := range movements {
		if m.From != wallet && m.To != wallet {
			continue
		}
		matches := 0
		for _, f := range fills {
			a := f.Attribution
			if f.Token.Address != token || f.PoolAddress != pool || f.Position.TransactionHash != m.TransactionHash || a.Quality == "unavailable" || a.Payer == nil || a.Recipient == nil || *a.Payer != wallet || *a.Recipient != wallet || a.TransactionSender != wallet || a.Trader != nil && *a.Trader != wallet {
				continue
			}
			kind := "output-transfer"
			from, to := pool, wallet
			if f.Side == "sell" {
				kind = "input-transfer"
				from, to = wallet, pool
			}
			if m.From != from || m.To != to || m.Amount != f.TokenAmountRaw || m.Height != func() int64 { n, _ := strconv.ParseInt(f.Position.Height, 10, 64); return n }() || m.BlockHash != f.Position.BlockHash {
				continue
			}
			for _, e := range a.Evidence {
				if e.Kind == kind && e.ArtifactID == m.TransactionHash && e.LogIndex != nil && *e.LogIndex == strconv.FormatUint(uint64(m.LogIndex), 10) && e.ArtifactSHA256 == f.Provenance.ReceiptSHA256 {
					matches++
					if used[f.SourceFillID] {
						return nil, ErrCostAllocationUnavailable
					}
					used[f.SourceFillID] = true
				}
			}
		}
		if matches != 1 {
			return nil, ErrCostAllocationUnavailable
		}
		q, err := accounting.Uint(m.Amount)
		if err != nil {
			return nil, err
		}
		if m.To == wallet {
			balance.Add(balance, q)
		} else {
			balance.Sub(balance, q)
		}
		if balance.Sign() < 0 || balance.BitLen() > 256 {
			return nil, delivery.ErrIntegrity
		}
	}
	for _, f := range fills {
		actors := f.Attribution
		if f.Token.Address == token && (actors.Payer != nil && *actors.Payer == wallet || actors.Recipient != nil && *actors.Recipient == wallet) && !used[f.SourceFillID] {
			return nil, ErrCostAllocationUnavailable
		}
	}
	return balance, nil
}
