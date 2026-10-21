// Package consumer implements the Redis pub/sub swap consumer that feeds the
// candle builder, porting candles/src/engine/swap-consumer.ts. It subscribes to
// the indexer swap channel and forwards Swap events to the Builder.
package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
)

// SwapConsumer subscribes to the Redis swap channel and processes swaps through
// the candle builder.
type SwapConsumer struct {
	builder    *engine.Builder
	subscriber *sharedredis.Subscriber
	logger     *slog.Logger
}

// New creates a SwapConsumer.
func New(builder *engine.Builder, redisURL string, logger *slog.Logger) (*SwapConsumer, error) {
	sub, err := sharedredis.NewSubscriber(redisURL)
	if err != nil {
		return nil, fmt.Errorf("consumer: new subscriber: %w", err)
	}
	return &SwapConsumer{
		builder:    builder,
		subscriber: sub,
		logger:     logger,
	}, nil
}

// Start subscribes to the swap channel and processes events until ctx is cancelled.
func (sc *SwapConsumer) Start(ctx context.Context) error {
	errc, err := sc.subscriber.Subscribe(ctx, constants.ChannelSwap, func(ctx context.Context, payload []byte) error {
		var data struct {
			EventName      string                 `json:"eventName"`
			BlockNumber    int64                  `json:"blockNumber"`
			BlockTimestamp int64                  `json:"blockTimestamp"`
			TxHash         string                 `json:"txHash"`
			LogIndex       int                    `json:"logIndex"`
			Args           map[string]interface{} `json:"args"`
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			sc.logger.Error("consumer: unmarshal swap", slog.String("err", err.Error()))
			return fmt.Errorf("consumer: invalid swap envelope: %w", err)
		}

		swap := engine.SwapEvent{
			PoolID:      asString(data.Args["poolId"]),
			PoolAddress: asString(data.Args["poolAddress"]),
			Sender:      asString(data.Args["sender"]),
			IsBuy:       asBool(data.Args["isBuy"]),
			AmountIn:    asString(data.Args["amountIn"]),
			AmountOut:   asString(data.Args["amountOut"]),
			Fee:         asString(data.Args["fee"]),
			Price:       asString(data.Args["price"]),
			TxHash:      data.TxHash,
			LogIndex:    data.LogIndex,
		}

		// Resolve the block timestamp. The indexer envelope (identical on the Redis
		// and webhook paths) carries it top-level as blockTimestamp; fall back to
		// args.blockTimestamp / args.timestamp for legacy message shapes. A height
		// is never a timestamp.
		switch {
		case data.BlockTimestamp > 0:
			swap.BlockTimestamp = data.BlockTimestamp
		case asInt64(data.Args["blockTimestamp"]) > 0:
			swap.BlockTimestamp = asInt64(data.Args["blockTimestamp"])
		case asInt64(data.Args["timestamp"]) > 0:
			swap.BlockTimestamp = asInt64(data.Args["timestamp"])
		default:
			return fmt.Errorf("consumer: authenticated block timestamp unavailable")
		}

		if err := sc.builder.ProcessSwap(ctx, swap); err != nil {
			return fmt.Errorf("consumer: process swap: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("consumer: subscribe: %w", err)
	}

	sc.logger.Info("candle swap consumer started")

	go func() {
		for err := range errc {
			sc.logger.Error("consumer: receive error", slog.String("err", err.Error()))
		}
	}()

	return nil
}

// Close releases the subscriber connection.
func (sc *SwapConsumer) Close() error {
	return sc.subscriber.Close()
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%v", val)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}

func asInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0
		}
		return n
	case string:
		var n int64
		if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

var ErrAccountingAuthority = errors.New("charts: accounting authority rejected")

type AccountingConfig struct {
	Deliveries                                *delivery.Store
	Partitions                                *delivery.PartitionStore
	Exact                                     *store.ExactStore
	ConsumerID, Generation, ProjectionVersion string
	Lease                                     func(context.Context, accounting.FillV1) (delivery.PartitionLease, error)
	Authorize                                 func(context.Context, pgx.Tx, accounting.FillV1) error
	AuthorizePublication                      func(context.Context, pgx.Tx, accounting.PublicationV1) error
	StreamWriter                              *delivery.StreamWriter
	SourceStream                              string
	SeriesCheckpoint                          func(context.Context, pgx.Tx, accounting.PublicationV1) (engine.SeriesCheckpoint, error)
}
type AccountingConsumer struct {
	config    AccountingConfig
	reprocess *ReprocessRequest
}

func NewAccountingConsumer(config AccountingConfig) (*AccountingConsumer, error) {
	if config.Deliveries == nil || config.Partitions == nil || config.Exact == nil || config.ConsumerID == "" || config.Generation == "" || config.ProjectionVersion == "" || config.Lease == nil || config.Authorize == nil {
		return nil, errors.New("charts: complete accounting authority and ordered stores required")
	}
	return &AccountingConsumer{config: config}, nil
}
func (c *AccountingConsumer) Consume(ctx context.Context, message *natsdriver.Message) error {
	if c == nil || message == nil || message.Subject != accounting.MarketSubject || message.Stream == "" || message.Sequence == 0 || message.EventID == "" {
		return errors.New("charts: real bounded market broker message required")
	}
	cfg := c.config
	if c.reprocess != nil {
		if message.EventID != c.reprocess.PublicationID || delivery.Hash(message.Wire) != c.reprocess.CorrectedDigest {
			return ErrAccountingAuthority
		}
		if err := cfg.Exact.CheckReprocess(ctx, cfg.ConsumerID, cfg.Generation, message.EventID, delivery.Hash(message.Wire)); err != nil {
			return err
		}
	}
	if cfg.SeriesCheckpoint != nil {
		if message.Sequence > uint64(^uint64(0)>>1) {
			return ErrAccountingAuthority
		}
		if err := cfg.Exact.RecordAttempt(ctx, cfg.ConsumerID, cfg.Generation, message.EventID, delivery.Hash(message.Wire), int64(message.Sequence), "received", false); err != nil {
			if errors.Is(err, store.ErrIntegrity) {
				pools := []string{}
				var envelope delivery.Envelope
				if json.Unmarshal(message.Wire, &envelope) == nil {
					publication, decodeErr := accounting.DecodePublicationV1(envelope.Payload)
					if decodeErr == nil && publication.Fill != nil {
						fill := publication.Fill
						pools = append(pools, fill.PoolAddress)
						var originalPool string
						lookup := cfg.Exact.Pool.QueryRow(ctx, `SELECT pool FROM `+cfg.Exact.Schema+`.chart_inbox WHERE chain_id='125' AND emitter=$1 AND code_epoch=$2 AND tx_hash=$3 AND log_index=$4::numeric`, fill.ActualEmitter, fill.Provenance.EmitterCodeEpoch, fill.Position.TransactionHash, fill.Position.LogIndex).Scan(&originalPool)
						if lookup != nil && !errors.Is(lookup, pgx.ErrNoRows) {
							return errors.Join(err, lookup)
						}
						if lookup == nil && originalPool != fill.PoolAddress {
							pools = append(pools, originalPool)
						}
					}
				}
				in := delivery.QuarantineInput{ConsumerID: cfg.ConsumerID, Generation: cfg.Generation, Subject: message.Subject, PublicationID: message.EventID, Wire: message.Wire, WireSHA256: accounting.Digest(delivery.Hash(message.Wire)), Class: delivery.IdentityConflict, AffectedPools: pools, EvidenceDigest: accounting.Digest(delivery.Hash(message.Wire))}
				qerr := cfg.Deliveries.QuarantineAfterRollback(ctx, in, func(ctx context.Context, tx pgx.Tx, affected []string) error {
					return cfg.Exact.FreezePoolsTx(ctx, tx, affected, in.EvidenceDigest)
				})
				return errors.Join(err, qerr)
			}
			return err
		}
		if err := cfg.Exact.WaitRetryBackoff(ctx, cfg.ConsumerID, cfg.Generation, message.EventID); err != nil {
			return err
		}
	}
	decode := func(envelope delivery.Envelope) (accounting.PublicationV1, error) {
		publication, err := accounting.DecodePublicationV1(envelope.Payload)
		if err != nil {
			return publication, &delivery.IntegrityFailure{Class: delivery.InnerSchema, Cause: err}
		}
		pools := []string{}
		if publication.Fill != nil {
			pools = append(pools, publication.Fill.PoolAddress)
		}
		if publication.Facet != "market" || publication.Fill == nil || publication.Enrichment != nil || len(publication.Positions) != 1 || publication.Positions[0].ProjectionVersion != cfg.ProjectionVersion || publication.Positions[0].PartitionKind != "market" || publication.Positions[0].PartitionKey != "125:"+publication.Fill.PoolAddress || publication.PublicationID != message.EventID || publication.PublicationID != envelope.EventID {
			return publication, &delivery.IntegrityFailure{Class: delivery.PartitionBinding, AffectedPools: pools, Cause: delivery.ErrIntegrity}
		}
		return publication, nil
	}
	hooks := delivery.ConsumeHooks{
		Guard: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope) (delivery.GuardedPosition, error) {
			publication, err := decode(envelope)
			if err != nil {
				return delivery.GuardedPosition{}, err
			}
			lease, err := cfg.Lease(ctx, *publication.Fill)
			if err != nil {
				return delivery.GuardedPosition{}, err
			}
			if lease.Key.ConsumerID != cfg.ConsumerID || lease.Key.ProjectionGeneration != cfg.Generation {
				return delivery.GuardedPosition{}, delivery.ErrLease
			}
			guard, err := cfg.Partitions.CheckAndLockTx(ctx, tx, lease, publication.Positions[0])
			if err != nil {
				return guard, err
			}
			if err = cfg.Authorize(ctx, tx, *publication.Fill); err != nil {
				if !errors.Is(err, ErrAccountingAuthority) {
					return guard, err
				}
				return guard, &delivery.IntegrityFailure{Class: delivery.Authority, AffectedPools: []string{publication.Fill.PoolAddress}, EvidenceDigest: accounting.Digest(delivery.Hash(envelope.Payload)), Cause: err}
			}
			if cfg.AuthorizePublication != nil {
				if err = cfg.AuthorizePublication(ctx, tx, publication); err != nil {
					if !errors.Is(err, ErrAccountingAuthority) {
						return guard, err
					}
					return guard, &delivery.IntegrityFailure{Class: delivery.Authority, AffectedPools: []string{publication.Fill.PoolAddress}, Cause: err}
				}
			}
			return guard, nil
		},
		Effect: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope, g delivery.GuardedPosition) error {
			publication, err := decode(envelope)
			if err != nil {
				return err
			}
			if cfg.SeriesCheckpoint != nil {
				if err := cfg.Exact.RecordAttemptResultTx(ctx, tx, cfg.ConsumerID, cfg.Generation, message.EventID, "applying", true); err != nil {
					return err
				}
				checkpoint, err := cfg.SeriesCheckpoint(ctx, tx, publication)
				if err != nil {
					return err
				}
				if err = engine.ApplySeriesStreamTx(ctx, tx, cfg.Exact, *publication.Fill, g.Lease.Epoch, checkpoint, cfg.StreamWriter, publication.Positions[0], cfg.SourceStream); err != nil {
					if errors.Is(err, store.ErrSourceWatermarkRegression) {
						return &delivery.IntegrityFailure{Class: delivery.IdentityConflict, AffectedPools: []string{publication.Fill.PoolAddress}, Cause: err}
					}
					return err
				}
				return nil
			}
			return engine.ApplyQualifiedTx(ctx, tx, cfg.Exact, *publication.Fill, g.Lease.Epoch)
		},
		Advance: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope, g delivery.GuardedPosition) error {
			if err := cfg.Partitions.AdvanceTx(ctx, tx, g); err != nil {
				return err
			}
			if cfg.SeriesCheckpoint != nil {
				return cfg.Exact.RecordAttemptResultTx(ctx, tx, cfg.ConsumerID, cfg.Generation, message.EventID, "committed", true)
			}
			return nil
		},
		Freeze: func(ctx context.Context, tx pgx.Tx, pools []string) error {
			return cfg.Exact.FreezePoolsTx(ctx, tx, pools, accounting.Digest(delivery.Hash(message.Wire)))
		},
	}
	err := cfg.Deliveries.ConsumeOrdered(ctx, cfg.ConsumerID, cfg.Generation, message.Subject, message.Wire, message, hooks)
	if err != nil && cfg.SeriesCheckpoint != nil {
		category := "dependency_unavailable"
		var integrity *delivery.IntegrityFailure
		if errors.As(err, &integrity) {
			category = string(integrity.Class)
		}
		if e := cfg.Exact.RecordAttemptResult(ctx, cfg.ConsumerID, cfg.Generation, message.EventID, category, false); e != nil {
			return errors.Join(err, e)
		}
	}
	return err
}
func (c *AccountingConsumer) Run(ctx context.Context, driver *natsdriver.Consumer) error {
	if driver == nil {
		return errors.New("charts: real broker driver required")
	}
	return driver.Run(ctx, c.Consume)
}

var ErrAccountingSourceUnavailable = errors.New("charts: canonical source unavailable or bootstrap replay exceeds bound")

type SourceVerifiedAccountingConfig struct {
	Pool                                                                                  *pgxpool.Pool
	Deliveries                                                                            *delivery.Store
	Partitions                                                                            *delivery.PartitionStore
	Exact                                                                                 *store.ExactStore
	ConsumerID, Generation, ProjectionVersion, Owner, SourceStream, SourcePartitionSchema string
	RPC                                                                                   *ethclient.Client
	MaxReplayBlocks, MaxReplayFills                                                       int
	SeriesV2                                                                              bool
	StreamWriter                                                                          *delivery.StreamWriter
}

func NewSourceVerifiedAccountingConsumer(config SourceVerifiedAccountingConfig) (*AccountingConsumer, error) {
	if config.Pool == nil || config.Deliveries == nil || config.Partitions == nil || config.Exact == nil || config.RPC == nil || config.Owner == "" || config.SourceStream == "" || len(config.SourceStream) > 128 || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(config.SourcePartitionSchema) || config.MaxReplayBlocks < 1 || config.MaxReplayBlocks > 4096 || config.MaxReplayFills < 1 || config.MaxReplayFills > 10000 {
		return nil, errors.New("charts: concrete canonical RPC/source stores and bounded replay configuration required")
	}
	if config.SeriesV2 && (config.StreamWriter == nil || config.StreamWriter.Schema != "api_stream") {
		return nil, errors.New("charts: canonical candle stream writer required")
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
			return fmt.Errorf("%w: %v", ErrAccountingAuthority, err)
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
		transactionIndex, err := strconv.ParseInt(fill.Position.TransactionIndex, 10, 64)
		if err != nil {
			return err
		}
		logIndex, err := strconv.ParseInt(fill.Position.LogIndex, 10, 64)
		if err != nil {
			return err
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
		err = tx.QueryRow(ctx, `SELECT f.source_fact_digest=$2 AND f.payload=$3::jsonb AND f.pool_address=$4 AND r.height=$5 AND r.transaction_index=$6 AND r.block_hash=$7 AND r.emitter=$8 AND c.block_hash=r.block_hash AND c.fetch_complete AND c.required_decode_complete AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$9 AND a.previous_height<r.height AND a.last_complete_height>=r.height) FROM indexer.indexer_accounting_fills_v1 f JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=f.chain_id AND r.transaction_hash=f.transaction_hash AND r.log_index=f.log_index JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.stream_id=$9 AND c.height=r.height WHERE f.source_fill_id=$1 AND f.chain_id=125 AND f.transaction_hash=$10 AND f.log_index=$11`, fill.SourceFillID, string(digest), body, fill.PoolAddress, height, transactionIndex, fill.Position.BlockHash, fill.ActualEmitter, config.SourceStream, fill.Position.TransactionHash, logIndex).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return ErrAccountingAuthority
		}
		return err
	}
	bootstrapper := sourceBootstrapper{config: config, authorize: authorize}
	loadBootstrap := bootstrapper.load
	verifyBootstrap := bootstrapper.verify
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
				return ErrAccountingAuthority
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
	authorizePublication := func(ctx context.Context, tx pgx.Tx, publication accounting.PublicationV1) error {
		body, err := accounting.CanonicalPublicationV1(publication)
		if err != nil {
			return err
		}
		if len(publication.Positions) != 1 {
			return ErrAccountingAuthority
		}
		position := publication.Positions[0]
		sequence, err := strconv.ParseInt(position.Sequence, 10, 64)
		if err != nil {
			return err
		}
		positionWire, err := json.Marshal(position)
		if err != nil {
			return err
		}
		q := pgx.Identifier{config.SourcePartitionSchema}.Sanitize()
		var same bool
		err = tx.QueryRow(ctx, `SELECT p.payload=$2::jsonb AND EXISTS(SELECT 1 FROM `+q+`.indexer_market_partition_positions_v1 s WHERE s.projection_version=$3 AND s.partition_kind='market' AND s.partition_key=$4 AND s.sequence=$5 AND s.position=$6) FROM indexer.indexer_accounting_publications_v1 p WHERE p.publication_id=$1`, publication.PublicationID, body, position.ProjectionVersion, position.PartitionKey, sequence, positionWire).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return ErrAccountingAuthority
		}
		return err
	}
	var checkpoint func(context.Context, pgx.Tx, accounting.PublicationV1) (engine.SeriesCheckpoint, error)
	if config.SeriesV2 {
		checkpoint = func(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1) (engine.SeriesCheckpoint, error) {
			watermark, err := sourceState(ctx, tx)
			if err != nil {
				return engine.SeriesCheckpoint{}, err
			}
			q := pgx.Identifier{config.SourcePartitionSchema}.Sanitize()
			var head int64
			err = tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0) FROM `+q+`.indexer_market_partition_positions_v1 WHERE projection_version=$1 AND partition_kind='market' AND partition_key=$2`, config.ProjectionVersion, "125:"+p.Fill.PoolAddress).Scan(&head)
			if err != nil {
				return engine.SeriesCheckpoint{}, err
			}
			sequence, err := strconv.ParseInt(p.Positions[0].Sequence, 10, 64)
			if err != nil {
				return engine.SeriesCheckpoint{}, err
			}
			if sequence != head {
				var prior engine.SeriesCheckpoint
				prior.HasGaps = true
				err = tx.QueryRow(ctx, `SELECT watermark_height,watermark_timestamp FROM `+config.Exact.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1`, p.Fill.PoolAddress).Scan(&prior.Height, &prior.Timestamp)
				if errors.Is(err, pgx.ErrNoRows) {
					err = nil
				}
				return prior, err
			}
			bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			header, err := chainread.Reader{RPC: config.RPC}.Block(bounded, watermark)
			if err != nil {
				return engine.SeriesCheckpoint{}, err
			}
			var expected string
			err = tx.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete`, config.SourceStream, watermark).Scan(&expected)
			if err != nil {
				return engine.SeriesCheckpoint{}, err
			}
			if header.Hash != expected {
				return engine.SeriesCheckpoint{}, ErrAccountingAuthority
			}
			return engine.SeriesCheckpoint{Height: uint64(watermark), Timestamp: header.Time}, nil
		}
	}
	return NewAccountingConsumer(AccountingConfig{Deliveries: config.Deliveries, Partitions: config.Partitions, Exact: config.Exact, ConsumerID: config.ConsumerID, Generation: config.Generation, ProjectionVersion: config.ProjectionVersion, Lease: lease, Authorize: authorize, AuthorizePublication: authorizePublication, SeriesCheckpoint: checkpoint, StreamWriter: config.StreamWriter, SourceStream: config.SourceStream})
}
