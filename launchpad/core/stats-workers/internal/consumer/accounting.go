package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/projectionstats"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting/sourcebootstrap"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type AccountingConsumer struct {
	store                                     *delivery.Store
	partitions                                *delivery.PartitionStore
	repository                                *projectionstats.Repository
	authority                                 *projectionstats.AccountingSourceAuthority
	consumerID, generation, projectionVersion string
	canonicalPublication                      func(context.Context, pgx.Tx, accounting.PublicationV1) error
	marketEffect                              func(context.Context, pgx.Tx, delivery.Envelope) error
}

func NewAccountingConsumer(store *delivery.Store, partitions *delivery.PartitionStore, repository *projectionstats.Repository, authority *projectionstats.AccountingSourceAuthority, consumerID, generation, projectionVersion string) (*AccountingConsumer, error) {
	if store == nil || partitions == nil || repository == nil || authority == nil || consumerID == "" || generation == "" || projectionVersion == "" || len(projectionVersion) > 64 || len(consumerID)+len(generation)+1 > 64 {
		return nil, projectionstats.ErrInvalid
	}
	return &AccountingConsumer{store: store, partitions: partitions, repository: repository, authority: authority, consumerID: consumerID, generation: generation, projectionVersion: projectionVersion}, nil
}

func AccountingPublication(envelope delivery.Envelope, lease delivery.PartitionLease, consumerID, generation, projectionVersion string) (accounting.PublicationV1, error) {
	p, err := accounting.DecodePublicationV1(envelope.Payload)
	if err != nil {
		return p, &delivery.IntegrityFailure{Class: delivery.InnerSchema, Cause: err}
	}
	invalid := func() error {
		return &delivery.IntegrityFailure{Class: delivery.PartitionBinding, EvidenceDigest: accounting.Digest(delivery.Hash(envelope.Payload)), Cause: delivery.ErrIntegrity}
	}
	if envelope.EventID != p.PublicationID || envelope.EventType != "accounting."+p.Facet+".v1" || envelope.ChainID != 125 || lease.Key.ConsumerID != consumerID || lease.Key.ProjectionGeneration != generation ||
		lease.Key.PartitionKind != p.Facet || lease.Key.PartitionKey != p.Positions[0].PartitionKey || p.Positions[0].ProjectionVersion != projectionVersion ||
		envelope.SourceOrder.Height != p.Positions[0].SourceOrder.Height || envelope.SourceOrder.TransactionIndex != p.Positions[0].SourceOrder.TransactionIndex || envelope.SourceOrder.LogIndex != p.Positions[0].SourceOrder.LogIndex {
		return p, invalid()
	}
	return p, nil
}

func (c *AccountingConsumer) hooks(wire []byte, lease delivery.PartitionLease) delivery.ConsumeHooks {
	return delivery.ConsumeHooks{
		Guard: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) (delivery.GuardedPosition, error) {
			p, err := AccountingPublication(e, lease, c.consumerID, c.generation, c.projectionVersion)
			if err != nil {
				return delivery.GuardedPosition{}, err
			}
			guarded, err := c.partitions.CheckAndLockTx(ctx, tx, lease, p.Positions[0])
			if err != nil {
				return guarded, err
			}
			if _, err = c.authority.OriginalTx(ctx, tx, p); err != nil {
				if errors.Is(err, projectionstats.ErrInvalid) || errors.Is(err, projectionstats.ErrIntegrity) {
					return guarded, &delivery.IntegrityFailure{Class: delivery.Authority, EvidenceDigest: accounting.Digest(delivery.Hash(e.Payload)), Cause: err}
				}
				return guarded, err
			}
			if c.canonicalPublication != nil && p.Fill != nil {
				if err = c.canonicalPublication(ctx, tx, p); err != nil {
					if errors.Is(err, projectionstats.ErrIntegrity) {
						return guarded, &delivery.IntegrityFailure{Class: delivery.Authority, AffectedPools: []string{p.Fill.PoolAddress}, Cause: err}
					}
					return delivery.GuardedPosition{}, err
				}
			}
			return guarded, nil
		},
		Effect: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope, g delivery.GuardedPosition) error {
			p, err := AccountingPublication(e, lease, c.consumerID, c.generation, c.projectionVersion)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(g.Position, p.Positions[0]) {
				return &delivery.IntegrityFailure{Class: delivery.PartitionBinding, Cause: delivery.ErrIntegrity}
			}
			if p.Facet == "inventory" {
				_, err = c.authority.OriginalTx(ctx, tx, p)
				if err == nil {
					err = c.repository.RecordInventoryPublicationTx(ctx, tx, p)
				}
			} else {
				var in projectionstats.QualifiedApplyInput
				in, err = c.authority.ResolveTx(ctx, tx, p, c.consumerID+":"+c.generation)
				if err == nil {
					var result projectionstats.ApplyResult
					result, err = c.repository.ApplyQualifiedTx(ctx, tx, in)
					if err == nil {
						err = c.repository.RecordMarketPublicationTx(ctx, tx, p, result)
						if err == nil && c.marketEffect != nil {
							err = c.marketEffect(ctx, tx, e)
						}
					}
				}
			}
			var conflict *projectionstats.AccountingIdentityConflict
			if errors.As(err, &conflict) {
				return &delivery.IntegrityFailure{Class: delivery.IdentityConflict, AffectedPools: append([]string(nil), conflict.Pools...), EvidenceDigest: accounting.Digest(delivery.Hash(e.Payload)), Cause: err}
			}
			if errors.Is(err, projectionstats.ErrInvalid) || errors.Is(err, projectionstats.ErrIntegrity) {
				pools := []string{}
				if p.Fill != nil {
					pools = append(pools, p.Fill.PoolAddress)
				}
				return &delivery.IntegrityFailure{Class: delivery.Authority, AffectedPools: pools, EvidenceDigest: accounting.Digest(delivery.Hash(e.Payload)), Cause: err}
			}
			return err
		},
		Advance: func(ctx context.Context, tx pgx.Tx, _ delivery.Envelope, g delivery.GuardedPosition) error {
			return c.partitions.AdvanceTx(ctx, tx, g)
		},
		Freeze: func(ctx context.Context, tx pgx.Tx, pools []string) error {
			return c.repository.FreezePoolsTx(ctx, tx, pools, accounting.Digest(delivery.Hash(wire)))
		},
	}
}
func (c *AccountingConsumer) Consume(ctx context.Context, subject string, wire []byte, message delivery.Delivery, lease delivery.PartitionLease) error {
	if c == nil {
		return projectionstats.ErrInvalid
	}
	return c.store.ConsumeOrdered(ctx, c.consumerID, c.generation, subject, wire, message, c.hooks(wire, lease))
}

var ErrAccountingSourceUnavailable = errors.New("stats: canonical source unavailable or bootstrap replay exceeds bound")

type SourceVerifiedAccountingConfig struct {
	MarketEffect                                                                          func(context.Context, pgx.Tx, delivery.Envelope) error
	Pool                                                                                  *pgxpool.Pool
	Deliveries                                                                            *delivery.Store
	Partitions                                                                            *delivery.PartitionStore
	Repository                                                                            *projectionstats.Repository
	Authority                                                                             *projectionstats.AccountingSourceAuthority
	ConsumerID, Generation, ProjectionVersion, Owner, SourceStream, SourcePartitionSchema string
	RPC                                                                                   *ethclient.Client
	MaxReplayBlocks, MaxReplayFills                                                       int
}
type statsSourceBootstrap struct {
	Fill                  accounting.FillV1
	Position              accounting.PartitionPosition
	PublicationID         string
	InitialPreviousHeight int64
	FirstCommitID         string
	FirstCommitLastHeight int64
	FirstCommitBlockHash  string
	Checkpoint            accounting.Digest
}

// reportedBlockHash returns the block hash the node reports for height
// (eth_getBlockByNumber "hash"), the identity the indexer records.
func reportedBlockHash(ctx context.Context, rpc *ethclient.Client, height int64) (common.Hash, error) {
	if rpc == nil || height < 0 {
		return common.Hash{}, ErrAccountingSourceUnavailable
	}
	var raw *struct {
		Number *hexutil.Big `json:"number"`
		Hash   *common.Hash `json:"hash"`
	}
	if err := rpc.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeBig(big.NewInt(height)), false); err != nil {
		return common.Hash{}, err
	}
	if raw == nil {
		return common.Hash{}, ethereum.NotFound
	}
	if raw.Number == nil || raw.Hash == nil || *raw.Hash == (common.Hash{}) || raw.Number.ToInt().Cmp(big.NewInt(height)) != 0 {
		return common.Hash{}, projectionstats.ErrIntegrity
	}
	return *raw.Hash, nil
}

func NewSourceVerifiedAccountingConsumer(config SourceVerifiedAccountingConfig) (*SourceVerifiedAccountingConsumer, error) {
	if config.Pool == nil || config.Deliveries == nil || config.Partitions == nil || config.Repository == nil || config.Authority == nil || config.RPC == nil || config.Owner == "" || config.SourceStream == "" || len(config.SourceStream) > 128 || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(config.SourcePartitionSchema) || config.MaxReplayBlocks < 1 || config.MaxReplayBlocks > 4096 || config.MaxReplayFills < 1 || config.MaxReplayFills > 10000 {
		return nil, errors.New("stats: concrete canonical RPC/source stores and bounded replay configuration required")
	}
	var mu sync.Mutex
	initialized := map[string]bool{}
	sourceState := func(ctx context.Context, tx pgx.Tx) (int64, error) {
		var raw, typed, fence int64
		var owner string
		var alive bool
		err := tx.QueryRow(ctx, `SELECT w.raw_complete_height,w.typed_complete_height,l.fence_token,l.owner_instance,l.expires_at>clock_timestamp() FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1`, config.SourceStream).Scan(&raw, &typed, &fence, &owner, &alive)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrAccountingSourceUnavailable
		}
		if err != nil {
			return 0, err
		}
		if raw < 0 || raw != typed || fence <= 0 || owner == "" || !alive {
			return 0, ErrAccountingSourceUnavailable
		}
		return typed, nil
	}
	authorize := func(ctx context.Context, tx pgx.Tx, fill accounting.FillV1) error {
		if err := accounting.ValidateFillV1(fill); err != nil {
			return fmt.Errorf("%w: %v", projectionstats.ErrIntegrity, err)
		}
		watermark, err := sourceState(ctx, tx)
		if err != nil {
			return err
		}
		height, err := strconv.ParseInt(fill.Position.Height, 10, 64)
		if err != nil {
			return err
		}
		if height > watermark {
			return ErrAccountingSourceUnavailable
		}
		body, err := json.Marshal(fill)
		if err != nil {
			return err
		}
		digest, err := accounting.FillDigest(fill)
		if err != nil {
			return err
		}
		var same bool
		err = tx.QueryRow(ctx, `SELECT f.source_fact_digest=$2 AND f.payload=$3::jsonb AND f.pool_address=$4 AND r.height=$5 AND r.transaction_index=$6 AND r.block_hash=$7 AND r.emitter=$8 AND c.block_hash=r.block_hash AND c.fetch_complete AND c.required_decode_complete AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$9 AND a.previous_height<r.height AND a.last_complete_height>=r.height) FROM indexer.indexer_accounting_fills_v1 f JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=f.chain_id AND r.transaction_hash=f.transaction_hash AND r.log_index=f.log_index JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.stream_id=$9 AND c.height=r.height WHERE f.source_fill_id=$1 AND f.chain_id=125 AND f.transaction_hash=$10 AND f.log_index=$11`, fill.SourceFillID, string(digest), body, fill.PoolAddress, height, fill.Position.TransactionIndex, fill.Position.BlockHash, fill.ActualEmitter, config.SourceStream, fill.Position.TransactionHash, fill.Position.LogIndex).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return projectionstats.ErrIntegrity
		}
		return err
	}
	loadBootstrap := func(ctx context.Context, tx pgx.Tx, pool string) (statsSourceBootstrap, error) {
		var b statsSourceBootstrap
		positionWire, err := sourcebootstrap.FirstMarketPosition(ctx, tx, config.SourcePartitionSchema, config.ProjectionVersion, pool)
		if errors.Is(err, pgx.ErrNoRows) {
			return b, ErrAccountingSourceUnavailable
		}
		if err != nil {
			return b, err
		}
		if err = json.Unmarshal(positionWire, &b.Position); err != nil {
			return b, err
		}
		if accounting.ValidatePartitionPosition(b.Position) != nil || b.Position.Sequence != "1" || b.Position.PreviousEffectIdentity != "" || b.Position.PartitionKind != "market" || b.Position.PartitionKey != "125:"+pool || b.Position.ProjectionVersion != config.ProjectionVersion {
			return b, projectionstats.ErrIntegrity
		}
		publicationWire, err := sourcebootstrap.MarketPublication(ctx, tx, b.Position.SourceEventID, config.ProjectionVersion)
		if err != nil {
			return b, err
		}
		publication, err := accounting.DecodePublicationV1(publicationWire)
		if err != nil {
			return b, err
		}
		if publication.Fill == nil || len(publication.Positions) != 1 || publication.Positions[0] != b.Position {
			return b, projectionstats.ErrIntegrity
		}
		b.Fill = *publication.Fill
		b.PublicationID = publication.PublicationID
		if err = authorize(ctx, tx, b.Fill); err != nil {
			return b, err
		}
		b.InitialPreviousHeight, err = sourcebootstrap.InitialPreviousHeight(ctx, tx, config.SourceStream)
		if err != nil {
			return b, err
		}
		height, err := strconv.ParseInt(b.Fill.Position.Height, 10, 64)
		if err != nil {
			return b, err
		}
		commit, err := sourcebootstrap.CommitContaining(ctx, tx, config.SourceStream, height)
		if err != nil {
			return b, err
		}
		b.FirstCommitID, b.FirstCommitLastHeight, b.FirstCommitBlockHash = commit.ID, commit.LastCompleteHeight, commit.BlockHash
		if b.InitialPreviousHeight < 0 || height <= b.InitialPreviousHeight || b.FirstCommitLastHeight-b.InitialPreviousHeight > int64(config.MaxReplayBlocks) {
			return b, ErrAccountingSourceUnavailable
		}
		b.Checkpoint = sourcebootstrap.StatsEvidence{SourceStream: config.SourceStream, Pool: pool, ProjectionVersion: config.ProjectionVersion, PublicationID: b.PublicationID, PayloadDigest: b.Position.PayloadDigest, InitialPreviousHeight: b.InitialPreviousHeight, FirstCommitID: b.FirstCommitID, FirstCommitLastHeight: b.FirstCommitLastHeight, FirstCommitBlockHash: b.FirstCommitBlockHash}.Checkpoint()
		return b, nil
	}
	verifyBootstrap := func(ctx context.Context, tx pgx.Tx, b statsSourceBootstrap) error {
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		chain, err := config.RPC.ChainID(bounded)
		if err != nil {
			return err
		}
		if chain.Cmp(big.NewInt(125)) != 0 {
			return projectionstats.ErrIntegrity
		}
		code, err := config.RPC.CodeAt(bounded, common.HexToAddress(b.Fill.PoolAddress), big.NewInt(b.InitialPreviousHeight))
		if err != nil {
			return err
		}
		if len(code) != 0 {
			return fmt.Errorf("%w: pool predates admitted source replay", projectionstats.ErrIntegrity)
		}
		// The indexer commits the node-reported block hash; on chain 125 that
		// is the CometBFT hash, never go-ethereum's recomputed Header.Hash().
		reported, err := reportedBlockHash(bounded, config.RPC, b.FirstCommitLastHeight)
		if err != nil {
			return err
		}
		if reported.Hex() != b.FirstCommitBlockHash {
			return projectionstats.ErrIntegrity
		}
		rows, err := tx.Query(bounded, `SELECT a.previous_height,a.last_complete_height,a.block_hash,a.record_count FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$1 AND a.previous_height>=$2 AND a.last_complete_height<=$3 ORDER BY a.previous_height LIMIT $4`, config.SourceStream, b.InitialPreviousHeight, b.FirstCommitLastHeight, config.MaxReplayBlocks+1)
		if err != nil {
			return err
		}
		type sourceRange struct {
			previous, last int64
			hash           string
			count          int
		}
		var ranges []sourceRange
		for rows.Next() {
			var r sourceRange
			if err = rows.Scan(&r.previous, &r.last, &r.hash, &r.count); err != nil {
				rows.Close()
				return err
			}
			ranges = append(ranges, r)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		next := b.InitialPreviousHeight
		for _, r := range ranges {
			if r.previous != next || r.last <= r.previous {
				return projectionstats.ErrIntegrity
			}
			var complete, raw int64
			var finalHash string
			err = tx.QueryRow(bounded, `SELECT count(*),COALESCE(sum(raw_count),0),(SELECT block_hash FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$3) FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height>$2 AND height<=$3 AND fetch_complete AND required_decode_complete`, config.SourceStream, r.previous, r.last).Scan(&complete, &raw, &finalHash)
			if err != nil {
				return err
			}
			if complete != r.last-r.previous || raw != int64(r.count) || finalHash != r.hash {
				return projectionstats.ErrIntegrity
			}
			next = r.last
		}
		if next != b.FirstCommitLastHeight {
			return projectionstats.ErrIntegrity
		}
		height, _ := strconv.ParseInt(b.Fill.Position.Height, 10, 64)
		signature := crypto.Keccak256Hash([]byte("Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)"))
		logs, err := config.RPC.FilterLogs(bounded, ethereum.FilterQuery{FromBlock: big.NewInt(b.InitialPreviousHeight + 1), ToBlock: big.NewInt(height), Topics: [][]common.Hash{{signature}, {common.BytesToHash(common.HexToAddress(b.Fill.PoolAddress).Bytes())}}})
		if err != nil {
			return err
		}
		if len(logs) > config.MaxReplayFills {
			return ErrAccountingSourceUnavailable
		}
		var first *types.Log
		for i := range logs {
			log := logs[i]
			if log.Removed {
				return projectionstats.ErrIntegrity
			}
			position := accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(log.BlockNumber, 10), TransactionIndex: strconv.FormatUint(uint64(log.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(log.Index), 10), TransactionHash: log.TxHash.Hex(), BlockHash: log.BlockHash.Hex()}
			comparison, err := accounting.CompareSourcePosition(position, b.Fill.Position)
			if err != nil {
				return err
			}
			if comparison < 0 {
				return fmt.Errorf("%w: replay found omitted earlier pool fill", projectionstats.ErrIntegrity)
			}
			if comparison == 0 {
				if first != nil {
					return projectionstats.ErrIntegrity
				}
				copy := log
				first = &copy
			}
		}
		if first == nil {
			return projectionstats.ErrIntegrity
		}
		topics := make([]string, len(first.Topics))
		for i, topic := range first.Topics {
			topics[i] = topic.Hex()
		}
		encodedTopics, err := json.Marshal(topics)
		if err != nil {
			return err
		}
		var same bool
		err = tx.QueryRow(bounded, `SELECT topics=$4::jsonb AND data=$5 AND block_hash=$6 AND emitter=$7 FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2 AND height=$3`, first.TxHash.Hex(), int64(first.Index), int64(first.BlockNumber), encodedTopics, first.Data, first.BlockHash.Hex(), strings.ToLower(first.Address.Hex())).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return projectionstats.ErrIntegrity
		}
		var count int
		err = tx.QueryRow(bounded, `SELECT count(*) FROM indexer.indexer_accounting_fills_v1 f JOIN indexer.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) WHERE f.pool_address=$1 AND (r.height,r.transaction_index,r.log_index)<=($2::bigint,$3::bigint,$4::bigint)`, b.Fill.PoolAddress, height, b.Fill.Position.TransactionIndex, b.Fill.Position.LogIndex).Scan(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			return projectionstats.ErrIntegrity
		}
		return nil
	}
	lease := func(ctx context.Context, fill accounting.FillV1) (delivery.PartitionLease, error) {
		mu.Lock()
		defer mu.Unlock()
		key := delivery.ConsumerKey{ConsumerID: config.ConsumerID, ProjectionGeneration: config.Generation, PartitionKind: "market", PartitionKey: "125:" + fill.PoolAddress}
		l, err := config.Partitions.Acquire(ctx, key, config.Owner, 30*time.Second)
		if err != nil {
			return l, err
		}
		if initialized[fill.PoolAddress] {
			return l, nil
		}
		tx, err := config.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			return l, err
		}
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			tx.Rollback(cleanup)
		}()
		b, err := loadBootstrap(ctx, tx, fill.PoolAddress)
		if err != nil {
			return l, err
		}
		input := delivery.BootstrapInput{Lease: l, CheckpointDigest: b.Checkpoint, NextSequence: "1", PreviousEffectIdentity: ""}
		err = config.Partitions.BootstrapTx(ctx, tx, input, func(ctx context.Context, tx pgx.Tx, in delivery.BootstrapInput) error {
			if in.CheckpointDigest != b.Checkpoint || in.NextSequence != "1" || in.PreviousEffectIdentity != "" {
				return projectionstats.ErrIntegrity
			}
			return verifyBootstrap(ctx, tx, b)
		})
		if err != nil {
			return l, err
		}
		if err = tx.Commit(ctx); err != nil {
			return l, err
		}
		initialized[fill.PoolAddress] = true
		return l, nil
	}
	consumer, err := NewAccountingConsumer(config.Deliveries, config.Partitions, config.Repository, config.Authority, config.ConsumerID, config.Generation, config.ProjectionVersion)
	if err != nil {
		return nil, err
	}
	consumer.marketEffect = config.MarketEffect
	consumer.canonicalPublication = func(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1) error {
		if p.Fill == nil || p.Facet != "market" || len(p.Positions) != 1 {
			return projectionstats.ErrIntegrity
		}
		if err := authorize(ctx, tx, *p.Fill); err != nil {
			return err
		}
		wire, err := json.Marshal(p)
		if err != nil {
			return err
		}
		position, err := json.Marshal(p.Positions[0])
		if err != nil {
			return err
		}
		q := pgx.Identifier{config.SourcePartitionSchema}.Sanitize()
		var same bool
		err = tx.QueryRow(ctx, `SELECT p.payload=$2::jsonb AND convert_from(s.position,'UTF8')::jsonb=$3::jsonb FROM indexer.indexer_accounting_publications_v1 p JOIN `+q+`.indexer_market_partition_positions_v1 s ON s.projection_version=$4 AND s.partition_kind='market' AND s.partition_key=$5 AND s.sequence=$6 WHERE p.publication_id=$1`, p.PublicationID, wire, position, config.ProjectionVersion, p.Positions[0].PartitionKey, p.Positions[0].Sequence).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return projectionstats.ErrIntegrity
		}
		return err
	}
	return &SourceVerifiedAccountingConsumer{consumer: consumer, lease: lease}, nil
}

type SourceVerifiedAccountingConsumer struct {
	consumer *AccountingConsumer
	lease    func(context.Context, accounting.FillV1) (delivery.PartitionLease, error)
}

func (c *SourceVerifiedAccountingConsumer) Consume(ctx context.Context, message *natsdriver.Message) error {
	return c.ConsumeBatch(ctx, []*natsdriver.Message{message})
}

func (c *SourceVerifiedAccountingConsumer) Run(ctx context.Context, driver *natsdriver.Consumer) error {
	if c == nil || driver == nil {
		return projectionstats.ErrInvalid
	}
	for {
		messages, err := driver.Fetch(ctx)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			continue
		}
		bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = c.ConsumeBatch(bounded, messages)
		cancel()
		if err != nil {
			return err
		}
	}
}

func (c *SourceVerifiedAccountingConsumer) ConsumeBatch(ctx context.Context, messages []*natsdriver.Message) error {
	if c == nil || c.consumer == nil || c.lease == nil || ctx == nil || len(messages) < 1 || len(messages) > 64 {
		return projectionstats.ErrInvalid
	}
	type item struct {
		message     *natsdriver.Message
		envelope    delivery.Envelope
		publication accounting.PublicationV1
	}
	reject := func(message *natsdriver.Message, eventID string, class delivery.FailureClass, pools []string, cause error) error {
		failure := &delivery.IntegrityFailure{Class: class, AffectedPools: pools, EvidenceDigest: accounting.Digest(delivery.Hash(message.Wire)), Cause: cause}
		q := delivery.QuarantineInput{ConsumerID: c.consumer.consumerID, Generation: c.consumer.generation, Subject: message.Subject, PublicationID: eventID, Wire: message.Wire, WireSHA256: accounting.Digest(delivery.Hash(message.Wire)), Class: class, AffectedPools: pools, EvidenceDigest: failure.EvidenceDigest}
		freeze := func(ctx context.Context, tx pgx.Tx, affected []string) error {
			return c.consumer.repository.FreezePoolsTx(ctx, tx, affected, failure.EvidenceDigest)
		}
		return errors.Join(failure, c.consumer.store.QuarantineAfterRollback(ctx, q, freeze))
	}
	items := make([]item, len(messages))
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"accounting.market.v1": true}}
	seen := map[string]item{}
	for i, m := range messages {
		if m == nil || m.Subject != accounting.MarketSubject {
			return projectionstats.ErrInvalid
		}
		broker, err := m.BrokerPosition()
		if err != nil {
			return err
		}
		e, err := delivery.Decode(m.Wire, policy, m.Subject)
		if err != nil {
			return reject(m, "", delivery.InnerSchema, nil, err)
		}
		p, err := accounting.DecodePublicationV1(e.Payload)
		if err != nil {
			return reject(m, e.EventID, delivery.InnerSchema, nil, err)
		}
		pools := []string{}
		if p.Fill != nil {
			pools = append(pools, p.Fill.PoolAddress)
		}
		if p.Fill == nil || p.Facet != "market" || len(p.Positions) != 1 || p.PublicationID != e.EventID || broker.EventID != e.EventID || broker.Subject != m.Subject || broker.WireSHA256 != delivery.Hash(m.Wire) {
			return reject(m, e.EventID, delivery.PartitionBinding, pools, projectionstats.ErrIntegrity)
		}
		if previous, ok := seen[e.EventID]; ok && delivery.Hash(previous.message.Wire) != delivery.Hash(m.Wire) {
			pools = append(pools, previous.publication.Fill.PoolAddress)
			return reject(m, e.EventID, delivery.IdentityConflict, pools, projectionstats.ErrIntegrity)
		}
		items[i] = item{m, e, p}
		seen[e.EventID] = items[i]
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].publication.Positions[0], items[j].publication.Positions[0]
		if a.PartitionKey != b.PartitionKey {
			return a.PartitionKey < b.PartitionKey
		}
		if len(a.Sequence) != len(b.Sequence) {
			return len(a.Sequence) < len(b.Sequence)
		}
		return a.Sequence < b.Sequence
	})
	leases := map[string]delivery.PartitionLease{}
	for _, v := range items {
		pool := v.publication.Fill.PoolAddress
		if _, ok := leases[pool]; ok {
			continue
		}
		lease, err := c.lease(ctx, *v.publication.Fill)
		if err != nil {
			return err
		}
		leases[pool] = lease
	}
	inputs := make([]delivery.OrderedDelivery, len(items))
	byID := map[string]item{}
	for i, v := range items {
		inputs[i] = delivery.OrderedDelivery{Subject: v.message.Subject, Wire: v.message.Wire, Delivery: v.message}
		byID[v.envelope.EventID] = v
	}
	failure := func(err error, e delivery.Envelope, pools []string) error {
		var conflict *projectionstats.AccountingIdentityConflict
		if errors.As(err, &conflict) {
			err = &delivery.IntegrityFailure{Class: delivery.IdentityConflict, AffectedPools: append([]string(nil), conflict.Pools...), EvidenceDigest: accounting.Digest(delivery.Hash(e.Payload)), Cause: err}
		} else if errors.Is(err, projectionstats.ErrInvalid) || errors.Is(err, projectionstats.ErrIntegrity) {
			err = &delivery.IntegrityFailure{Class: delivery.Authority, AffectedPools: pools, EvidenceDigest: accounting.Digest(delivery.Hash(e.Payload)), Cause: err}
		}
		return &delivery.OrderedBatchFailure{EventID: e.EventID, Cause: err}
	}
	hooks := delivery.OrderedBatchHooks{
		Guard: func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) (delivery.GuardedPosition, error) {
			v, ok := byID[e.EventID]
			if !ok {
				return delivery.GuardedPosition{}, delivery.ErrIntegrity
			}
			return c.consumer.hooks(v.message.Wire, leases[v.publication.Fill.PoolAddress]).Guard(ctx, tx, e)
		},
		Effect: func(ctx context.Context, tx pgx.Tx, effects []delivery.OrderedEffect) error {
			qualified := make([]projectionstats.QualifiedApplyInput, len(effects))
			publications := make([]accounting.PublicationV1, len(effects))
			pools := make([]string, 0, len(effects))
			for i, effect := range effects {
				v := byID[effect.Envelope.EventID]
				publications[i] = v.publication
				pools = append(pools, v.publication.Fill.PoolAddress)
				var err error
				qualified[i], err = c.consumer.authority.ResolveTx(ctx, tx, v.publication, c.consumer.consumerID+":"+c.consumer.generation)
				if err != nil {
					return failure(err, effect.Envelope, []string{v.publication.Fill.PoolAddress})
				}
			}
			results, err := c.consumer.repository.ApplyQualifiedBatchTx(ctx, tx, qualified)
			if err != nil {
				index := 0
				var specific *projectionstats.BatchApplyError
				if errors.As(err, &specific) && specific.Index >= 0 && specific.Index < len(effects) {
					index = specific.Index
				}
				return failure(err, effects[index].Envelope, pools)
			}
			for i, effect := range effects {
				p := publications[i]
				if err = c.consumer.repository.RecordMarketPublicationTx(ctx, tx, p, results[i]); err != nil {
					return failure(err, effect.Envelope, []string{p.Fill.PoolAddress})
				}
				if c.consumer.marketEffect != nil {
					if err = c.consumer.marketEffect(ctx, tx, effect.Envelope); err != nil {
						return failure(err, effect.Envelope, []string{p.Fill.PoolAddress})
					}
				}
			}
			return nil
		},
		Freeze: func(ctx context.Context, tx pgx.Tx, pools []string) error {
			return c.consumer.repository.FreezePoolsTx(ctx, tx, pools, accounting.Digest(delivery.Hash(inputs[0].Wire)))
		},
	}
	return c.consumer.store.ConsumeOrderedBatch(ctx, c.consumer.consumerID, c.consumer.generation, inputs, hooks)
}
