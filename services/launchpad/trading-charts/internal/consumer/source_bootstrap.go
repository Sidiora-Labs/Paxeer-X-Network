package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting/sourcebootstrap"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
)

// maxBootstrapReplayWindows bounds the source replay a pool bootstrap performs
// between the pool's creation and its first fill: at most this many RPC log
// windows of MaxReplayBlocks blocks each.
const maxBootstrapReplayWindows = 4096

// swapEventTopic is the Sidiora event hub Swap signature; topic1 is the pool.
var swapEventTopic = crypto.Keccak256Hash([]byte("Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)"))

// poolCreation is the indexer's canonical MarketCreated fact for a pool joined
// to the raw log it was decoded from.
type poolCreation struct {
	Height, TransactionIndex, LogIndex int64
	BlockHash, TransactionHash         string
	Factory                            string
	Topics                             []string
	Data                               []byte
}

func (c poolCreation) position() accounting.SourcePosition {
	return accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(c.Height, 10), TransactionIndex: strconv.FormatInt(c.TransactionIndex, 10), LogIndex: strconv.FormatInt(c.LogIndex, 10), TransactionHash: c.TransactionHash, BlockHash: c.BlockHash}
}

// chartSourceBootstrap is the evidence that partition sequence 1 of a pool is
// the pool's first fill: the replay runs from the indexer commit boundary
// before the pool's creation block through the commit holding the first fill.
type chartSourceBootstrap struct {
	Fill                  accounting.FillV1
	Position              accounting.PartitionPosition
	PublicationID         string
	Creation              poolCreation
	ReplayFromHeight      int64
	FirstCommitID         string
	FirstCommitLastHeight int64
	FirstCommitBlockHash  string
	Checkpoint            accounting.Digest
}

type sourceBootstrapper struct {
	config    SourceVerifiedAccountingConfig
	authorize func(context.Context, pgx.Tx, accounting.FillV1) error
}

// load reads the bootstrap evidence for pool. A pool is bootstrapped from its
// own creation, so a pool created at any height after the indexer stream began
// is admitted; only the creation-to-first-fill span is bounded.
func (s sourceBootstrapper) load(ctx context.Context, tx pgx.Tx, pool string) (chartSourceBootstrap, error) {
	config := s.config
	var b chartSourceBootstrap
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
		return b, fmt.Errorf("%w: first partition position is not a market origin", ErrAccountingAuthority)
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
		return b, fmt.Errorf("%w: first publication does not carry the first position", ErrAccountingAuthority)
	}
	b.Fill = *publication.Fill
	b.PublicationID = publication.PublicationID
	if err = s.authorize(ctx, tx, b.Fill); err != nil {
		return b, err
	}
	creation, err := sourcebootstrap.CanonicalCreation(ctx, tx, config.SourceStream, pool)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrAccountingSourceUnavailable
	}
	if err != nil {
		return b, err
	}
	topics := creation.TopicsWire
	b.Creation.Height, b.Creation.BlockHash, b.Creation.TransactionHash, b.Creation.TransactionIndex, b.Creation.LogIndex, b.Creation.Factory, b.Creation.Data = creation.Height, creation.BlockHash, creation.TransactionHash, creation.TransactionIndex, creation.LogIndex, creation.Factory, creation.Data
	b.Creation.Factory = strings.ToLower(b.Creation.Factory)
	if err = json.Unmarshal(topics, &b.Creation.Topics); err != nil {
		return b, ErrAccountingAuthority
	}
	if !logNamesAddress(b.Creation.Topics, b.Creation.Data, pool) {
		return b, fmt.Errorf("%w: creation log does not name the pool", ErrAccountingAuthority)
	}
	order, err := accounting.CompareSourcePosition(b.Creation.position(), b.Fill.Position)
	if err != nil {
		return b, err
	}
	if order >= 0 {
		return b, fmt.Errorf("%w: first fill does not follow pool creation", ErrAccountingAuthority)
	}
	b.ReplayFromHeight, err = sourcebootstrap.PreviousHeightContaining(ctx, tx, config.SourceStream, b.Creation.Height)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrAccountingSourceUnavailable
	}
	if err != nil {
		return b, err
	}
	height, err := strconv.ParseInt(b.Fill.Position.Height, 10, 64)
	if err != nil {
		return b, err
	}
	commit, err := sourcebootstrap.CommitContaining(ctx, tx, config.SourceStream, height)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrAccountingSourceUnavailable
	}
	if err != nil {
		return b, err
	}
	b.FirstCommitID, b.FirstCommitLastHeight, b.FirstCommitBlockHash = commit.ID, commit.LastCompleteHeight, commit.BlockHash
	if b.ReplayFromHeight < 0 || b.ReplayFromHeight >= b.Creation.Height || height < b.Creation.Height || b.FirstCommitLastHeight < height || (height-b.Creation.Height)/int64(config.MaxReplayBlocks)+1 > maxBootstrapReplayWindows {
		return b, ErrAccountingSourceUnavailable
	}
	b.Checkpoint = sourcebootstrap.ChartsEvidence{SourceStream: config.SourceStream, Pool: pool, ProjectionVersion: config.ProjectionVersion, PublicationID: b.PublicationID, PayloadDigest: b.Position.PayloadDigest, ReplayFromHeight: b.ReplayFromHeight, CreationTransactionHash: b.Creation.TransactionHash, CreationLogIndex: b.Creation.LogIndex, FirstCommitID: b.FirstCommitID, FirstCommitLastHeight: b.FirstCommitLastHeight, FirstCommitBlockHash: b.FirstCommitBlockHash}.Checkpoint()
	return b, nil
}

// verify proves the bootstrap evidence against the chain and the indexer's
// completeness records: the creation log and the first fill are on the
// canonical chain, every block from the replay start through the first fill's
// commit was fully ingested, and the chain holds no earlier Swap for the pool.
func (s sourceBootstrapper) verify(ctx context.Context, tx pgx.Tx, b chartSourceBootstrap) error {
	config := s.config
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	chain, err := config.RPC.ChainID(bounded)
	if err != nil {
		return err
	}
	if chain.Cmp(big.NewInt(125)) != 0 {
		return ErrAccountingAuthority
	}
	creationBlock, err := chainread.Reader{RPC: config.RPC}.Block(bounded, b.Creation.Height)
	if err != nil {
		return err
	}
	if creationBlock.Hash != b.Creation.BlockHash {
		return ErrAccountingAuthority
	}
	receipt, err := config.RPC.TransactionReceipt(bounded, common.HexToHash(b.Creation.TransactionHash))
	if err != nil {
		return err
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.BlockHash.Hex() != b.Creation.BlockHash || receipt.BlockNumber == nil || receipt.BlockNumber.Int64() != b.Creation.Height || int64(receipt.TransactionIndex) != b.Creation.TransactionIndex {
		return fmt.Errorf("%w: creation receipt conflicts with indexed creation", ErrAccountingAuthority)
	}
	var created *types.Log
	for _, log := range receipt.Logs {
		if int64(log.Index) == b.Creation.LogIndex {
			created = log
		}
	}
	if created == nil || created.Removed || strings.ToLower(created.Address.Hex()) != b.Creation.Factory || !bytes.Equal(created.Data, b.Creation.Data) || len(created.Topics) != len(b.Creation.Topics) {
		return fmt.Errorf("%w: creation log conflicts with indexed raw log", ErrAccountingAuthority)
	}
	for i, topic := range created.Topics {
		if !strings.EqualFold(topic.Hex(), b.Creation.Topics[i]) {
			return fmt.Errorf("%w: creation log conflicts with indexed raw log", ErrAccountingAuthority)
		}
	}
	commitBlock, err := chainread.Reader{RPC: config.RPC}.Block(bounded, b.FirstCommitLastHeight)
	if err != nil {
		return err
	}
	if commitBlock.Hash != b.FirstCommitBlockHash {
		return ErrAccountingAuthority
	}
	var commits, violations, firstCommit, lastCommit int64
	err = tx.QueryRow(bounded, `WITH a AS (SELECT previous_height p,last_complete_height l,block_hash h,record_count n,lag(last_complete_height) OVER (ORDER BY previous_height,last_complete_height) prior FROM indexer.indexer_ingest_commits_v1 WHERE chain_id=125 AND stream_id=$1 AND previous_height>=$2 AND last_complete_height<=$3)
SELECT count(*),COALESCE(min(p),-1),COALESCE(max(l),-1),count(*) FILTER (WHERE l<=p OR (prior IS NOT NULL AND prior<>p)
 OR (SELECT count(*) FROM indexer.indexer_block_completeness_v1 c WHERE c.chain_id=125 AND c.stream_id=$1 AND c.height>a.p AND c.height<=a.l AND c.fetch_complete AND c.required_decode_complete)<>a.l-a.p
 OR (SELECT COALESCE(sum(c.raw_count),0) FROM indexer.indexer_block_completeness_v1 c WHERE c.chain_id=125 AND c.stream_id=$1 AND c.height>a.p AND c.height<=a.l AND c.fetch_complete AND c.required_decode_complete)<>a.n
 OR (SELECT count(*) FROM indexer.indexer_raw_logs_v1 r WHERE r.chain_id=125 AND r.height>a.p AND r.height<=a.l)<>a.n
 OR (SELECT c.block_hash FROM indexer.indexer_block_completeness_v1 c WHERE c.chain_id=125 AND c.stream_id=$1 AND c.height=a.l) IS DISTINCT FROM a.h) FROM a`, config.SourceStream, b.ReplayFromHeight, b.FirstCommitLastHeight).Scan(&commits, &firstCommit, &lastCommit, &violations)
	if err != nil {
		return err
	}
	if commits == 0 || violations != 0 || firstCommit != b.ReplayFromHeight || lastCommit != b.FirstCommitLastHeight {
		return fmt.Errorf("%w: source replay range is not completely ingested", ErrAccountingAuthority)
	}
	height, err := strconv.ParseInt(b.Fill.Position.Height, 10, 64)
	if err != nil {
		return err
	}
	firstBlock, err := chainread.Reader{RPC: config.RPC}.Block(bounded, height)
	if err != nil {
		return err
	}
	if firstBlock.Hash != b.Fill.Position.BlockHash || strconv.FormatInt(firstBlock.Time, 10) != b.Fill.BlockTimestampSeconds {
		return ErrAccountingAuthority
	}
	poolTopic := common.BytesToHash(common.HexToAddress(b.Fill.PoolAddress).Bytes())
	var logs []types.Log
	for from := b.Creation.Height; from <= height; from += int64(config.MaxReplayBlocks) {
		to := min(from+int64(config.MaxReplayBlocks)-1, height)
		window, stop := context.WithTimeout(ctx, 15*time.Second)
		found, err := config.RPC.FilterLogs(window, ethereum.FilterQuery{FromBlock: big.NewInt(from), ToBlock: big.NewInt(to), Topics: [][]common.Hash{{swapEventTopic}, {poolTopic}}})
		stop()
		if err != nil {
			return err
		}
		logs = append(logs, found...)
		if len(logs) > config.MaxReplayFills {
			return ErrAccountingSourceUnavailable
		}
	}
	var first *types.Log
	for i := range logs {
		log := logs[i]
		if log.Removed {
			return ErrAccountingAuthority
		}
		position := accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(log.BlockNumber, 10), TransactionIndex: strconv.FormatUint(uint64(log.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(log.Index), 10), TransactionHash: log.TxHash.Hex(), BlockHash: log.BlockHash.Hex()}
		comparison, err := accounting.CompareSourcePosition(position, b.Fill.Position)
		if err != nil {
			return err
		}
		if comparison < 0 {
			return fmt.Errorf("%w: replay found omitted earlier pool fill", ErrAccountingAuthority)
		}
		if comparison == 0 {
			if first != nil {
				return ErrAccountingAuthority
			}
			copy := log
			first = &copy
		}
	}
	if first == nil {
		return ErrAccountingAuthority
	}
	bounded, stopTail := context.WithTimeout(ctx, 15*time.Second)
	defer stopTail()
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
		return ErrAccountingAuthority
	}
	transactionIndex, err := strconv.ParseInt(b.Fill.Position.TransactionIndex, 10, 64)
	if err != nil {
		return err
	}
	logIndex, err := strconv.ParseInt(b.Fill.Position.LogIndex, 10, 64)
	if err != nil {
		return err
	}
	var count int
	err = tx.QueryRow(bounded, `SELECT count(*) FROM indexer.indexer_accounting_fills_v1 f JOIN indexer.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) WHERE f.pool_address=$1 AND (r.height,r.transaction_index,r.log_index)<=($2::bigint,$3::bigint,$4::bigint)`, b.Fill.PoolAddress, height, transactionIndex, logIndex).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrAccountingAuthority
	}
	return nil
}

// logNamesAddress reports whether address appears as a 32-byte word in the
// log's topics or data.
func logNamesAddress(topics []string, data []byte, address string) bool {
	word := common.BytesToHash(common.HexToAddress(address).Bytes())
	for _, topic := range topics {
		if strings.EqualFold(topic, word.Hex()) {
			return true
		}
	}
	for i := 0; i+32 <= len(data); i += 32 {
		if bytes.Equal(data[i:i+32], word.Bytes()) {
			return true
		}
	}
	return false
}
