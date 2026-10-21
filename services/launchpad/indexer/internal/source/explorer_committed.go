package source

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ExplorerSchemaV1 = "blockscout-canonical-v1"

type ExplorerProvenance struct {
	SourceID, SchemaVersion string
	IndexedHeight           uint64
	ValidatedCheckpoints    []BlockRecord
}
type CommittedPaxscanOptions struct {
	SourceID, SchemaVersion, ExpectedRole string
	MaxConnections                        int32
	ManifestDigest                        string
	Epochs                                []AddressEpoch
	RPC                                   *CommittedSource
	ParityAdmission                       *ParityReport
}
type ExplorerMetrics struct {
	Queries, SampleDisagreements uint64
	QueryNanoseconds             int64
}
type CommittedPaxscanSource struct {
	pool                   *pgxpool.Pool
	options                CommittedPaxscanOptions
	queries, disagreements atomic.Uint64
	queryNanos             atomic.Int64
	lifecycle              *sourceLifecycle
}

func validSourceID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func NewCommittedPaxscan(ctx context.Context, dsn string, o CommittedPaxscanOptions) (*CommittedPaxscanSource, error) {
	if !validSourceID(o.SourceID) || o.SchemaVersion != ExplorerSchemaV1 || o.ExpectedRole == "" || len(o.ExpectedRole) > 63 || o.RPC == nil || o.ManifestDigest != o.RPC.opts.ManifestDigest {
		return nil, sourceError(IdentityError, "explorer-config", nil)
	}
	if err := validateEpochs(o.Epochs, o.ManifestDigest); err != nil {
		return nil, err
	}
	o.Epochs = cloneEpochs(o.Epochs)
	if len(o.Epochs) != len(o.RPC.opts.Epochs) {
		return nil, sourceError(IdentityError, o.SourceID, nil)
	}
	for _, epoch := range o.Epochs {
		matched := false
		for _, approved := range o.RPC.opts.Epochs {
			retirementEqual := epoch.RetirementHeight == nil && approved.RetirementHeight == nil || epoch.RetirementHeight != nil && approved.RetirementHeight != nil && *epoch.RetirementHeight == *approved.RetirementHeight
			if strings.EqualFold(epoch.Address, approved.Address) && epoch.Role == approved.Role && epoch.ABIRevision == approved.ABIRevision && epoch.ActivationHeight == approved.ActivationHeight && epoch.CreationTxIndex == approved.CreationTxIndex && retirementEqual {
				matched = true
				break
			}
		}
		if !matched {
			return nil, sourceError(IdentityError, o.SourceID, nil)
		}
	}
	if o.MaxConnections == 0 {
		o.MaxConnections = 5
	}
	if o.MaxConnections < 1 || o.MaxConnections > 8 {
		return nil, sourceError(InvalidError, o.SourceID, nil)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, sourceError(InvalidError, o.SourceID, nil)
	}
	cfg.MaxConns = o.MaxConnections
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 15 * time.Second
	cfg.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		if _, err := connection.Exec(ctx, "SET default_transaction_read_only = on; SET statement_timeout = '15000'; SET lock_timeout = '2000'"); err != nil {
			return sourceError(IdentityError, o.SourceID, nil)
		}
		var role, readOnly string
		var elevated, writes bool
		err := connection.QueryRow(ctx, `SELECT current_user,current_setting('default_transaction_read_only'),r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls,
   EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('logs','blocks','transactions') AND (has_table_privilege(current_user,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR c.relowner=r.oid)) FROM pg_roles r WHERE r.rolname=current_user`).Scan(&role, &readOnly, &elevated, &writes)
		if err != nil || role != o.ExpectedRole || readOnly != "on" || elevated || writes {
			return sourceError(IdentityError, o.SourceID, nil)
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, sourceError(IdentityError, o.SourceID, nil)
	}
	source := &CommittedPaxscanSource{pool: pool, options: o, lifecycle: newSourceLifecycle()}
	if err = source.validateSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return source, nil
}
func (s *CommittedPaxscanSource) validateSchema(ctx context.Context) error {
	expected := map[string][]string{"logs": {"address_hash", "first_topic", "second_topic", "third_topic", "fourth_topic", "data", "transaction_hash", "block_hash", "block_number", "index", "transaction_index"}, "blocks": {"number", "hash", "parent_hash", "timestamp", "consensus"}, "transactions": {"hash", "block_hash", "from_address_hash", "index"}}
	rows, err := s.pool.Query(ctx, `SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND table_name=ANY($1::text[])`, []string{"logs", "blocks", "transactions"})
	if err != nil {
		return sourceError(IdentityError, s.Name(), nil)
	}
	defer rows.Close()
	columns := map[string]string{}
	for rows.Next() {
		var table, column, kind string
		if err = rows.Scan(&table, &column, &kind); err != nil {
			return sourceError(InvalidError, s.Name(), nil)
		}
		columns[table+"."+column] = kind
	}
	if rows.Err() != nil {
		return sourceError(InvalidError, s.Name(), nil)
	}
	for table, names := range expected {
		for _, column := range names {
			kind := columns[table+"."+column]
			want := "bytea"
			switch column {
			case "number", "block_number", "index", "transaction_index":
				want = "integer"
			case "timestamp":
				want = "timestamp with time zone"
			case "consensus":
				want = "boolean"
			}
			if kind != want && !(want == "integer" && kind == "bigint") {
				return sourceError(InvalidError, s.Name(), fmt.Errorf("unsupported schema column %s.%s", table, column))
			}
		}
	}
	var readable bool
	if err = s.pool.QueryRow(ctx, `SELECT bool_and(has_table_privilege(current_user,c.oid,'SELECT')) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=ANY($1::text[])`, []string{"logs", "blocks", "transactions"}).Scan(&readable); err != nil || !readable {
		return sourceError(IdentityError, s.Name(), nil)
	}
	return nil
}
func (s *CommittedPaxscanSource) Name() string { return s.options.SourceID }
func (s *CommittedPaxscanSource) Close() error {
	if s.lifecycle != nil {
		if err := s.lifecycle.close(); err != nil {
			return err
		}
	}
	s.pool.Close()
	return nil
}
func (s *CommittedPaxscanSource) Metrics() ExplorerMetrics {
	return ExplorerMetrics{s.queries.Load(), s.disagreements.Load(), s.queryNanos.Load()}
}
func (s *CommittedPaxscanSource) Head(ctx context.Context) (int64, error) {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return 0, err
		}
		defer done()
	}
	if s.options.RPC.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.options.RPC.lifecycle.begin(ctx)
		if err != nil {
			return 0, err
		}
		defer done()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var head *int64
	if err := s.pool.QueryRow(ctx, paxscanHeadSQL).Scan(&head); err != nil || head == nil || *head < 0 {
		return 0, sourceError(IncompleteError, s.Name(), nil)
	}
	return *head, nil
}
func (s *CommittedPaxscanSource) FetchLogs(ctx context.Context, o FetchOptions) (FetchResult, error) {
	return s.fetchLogs(ctx, o, false)
}
func (s *CommittedPaxscanSource) FetchParityLogs(ctx context.Context, o FetchOptions) (FetchResult, error) {
	return s.fetchLogs(ctx, o, true)
}
func (s *CommittedPaxscanSource) fetchLogs(ctx context.Context, o FetchOptions, diagnostic bool) (FetchResult, error) {
	var empty FetchResult
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return empty, err
		}
		defer done()
	}
	if o.FromBlock < 0 || o.ToBlock < o.FromBlock || o.ToBlock-o.FromBlock >= 2000 {
		return empty, sourceError(InvalidError, s.Name(), nil)
	}
	if s.options.RPC.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.options.RPC.lifecycle.begin(ctx)
		if err != nil {
			return empty, err
		}
		defer done()
	}
	if err := s.options.RPC.waitHistorical(ctx); err != nil {
		return empty, err
	}
	started := time.Now()
	s.queries.Add(1)
	defer func() { s.queryNanos.Add(time.Since(started).Nanoseconds()) }()
	read, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(read, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, sourceError(IncompleteError, s.Name(), nil)
	}
	defer tx.Rollback(context.Background())
	var indexed *int64
	if err = tx.QueryRow(read, paxscanHeadSQL).Scan(&indexed); err != nil || indexed == nil || *indexed < o.ToBlock {
		return empty, sourceError(IncompleteError, s.Name(), nil)
	}
	result := FetchResult{ManifestDigest: s.options.ManifestDigest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	blockRows, err := tx.Query(read, `SELECT number,hash,parent_hash,EXTRACT(EPOCH FROM timestamp)::bigint,EXTRACT(EPOCH FROM timestamp)=TRUNC(EXTRACT(EPOCH FROM timestamp)) FROM public.blocks WHERE consensus=TRUE AND number BETWEEN $1 AND $2 ORDER BY number`, o.FromBlock, o.ToBlock)
	if err != nil {
		return empty, sourceError(InvalidError, s.Name(), nil)
	}
	for blockRows.Next() {
		var height, ts int64
		var hash, parent []byte
		var integral bool
		if err = blockRows.Scan(&height, &hash, &parent, &ts, &integral); err != nil || !integral || height < o.FromBlock || height > o.ToBlock || ts <= 0 || len(hash) != 32 || len(parent) != 32 {
			blockRows.Close()
			return empty, sourceError(IncompleteError, s.Name(), nil)
		}
		if _, ok := result.Blocks[uint64(height)]; ok {
			blockRows.Close()
			return empty, sourceError(IntegrityError, s.Name(), nil)
		}
		result.Blocks[uint64(height)] = BlockRecord{ChainID: 125, Height: uint64(height), Hash: common.BytesToHash(hash), ParentHash: common.BytesToHash(parent), TimestampSeconds: ts, SourceID: s.Name()}
		result.BlockTimestamps[uint64(height)] = ts
	}
	err = blockRows.Err()
	blockRows.Close()
	if err != nil || len(result.Blocks) != int(o.ToBlock-o.FromBlock+1) {
		return empty, sourceError(IncompleteError, s.Name(), nil)
	}
	for height := uint64(o.FromBlock) + 1; height <= uint64(o.ToBlock); height++ {
		if result.Blocks[height].ParentHash != result.Blocks[height-1].Hash {
			return empty, sourceError(IntegrityError, s.Name(), nil)
		}
	}
	type origin struct {
		hash, block common.Hash
		index       uint
		from        common.Address
	}
	origins := map[common.Hash]origin{}
	positions := map[string]bool{}
	transactionRows, err := tx.Query(read, `SELECT t.hash,t.block_hash,t."index",t.from_address_hash FROM public.transactions t JOIN public.blocks b ON b.hash=t.block_hash WHERE b.consensus=TRUE AND b.number BETWEEN $1 AND $2 ORDER BY b.number,t."index"`, o.FromBlock, o.ToBlock)
	if err != nil {
		return empty, sourceError(InvalidError, s.Name(), nil)
	}
	byHash := map[common.Hash]uint64{}
	for height, block := range result.Blocks {
		byHash[block.Hash] = height
	}
	for transactionRows.Next() {
		var hash, block, from []byte
		var index int64
		if err = transactionRows.Scan(&hash, &block, &index, &from); err != nil || len(hash) != 32 || len(block) != 32 || len(from) != 20 || index < 0 || index > math.MaxUint32 {
			transactionRows.Close()
			return empty, sourceError(IncompleteError, s.Name(), nil)
		}
		h, bh := common.BytesToHash(hash), common.BytesToHash(block)
		height, ok := byHash[bh]
		position := fmt.Sprintf("%s/%d", bh, index)
		if !ok || positions[position] || origins[h].hash != (common.Hash{}) {
			transactionRows.Close()
			return empty, sourceError(IntegrityError, s.Name(), nil)
		}
		record := result.Blocks[height]
		if index != int64(record.TransactionCount) {
			transactionRows.Close()
			return empty, sourceError(IncompleteError, s.Name(), nil)
		}
		record.TransactionCount++
		result.Blocks[height] = record
		positions[position] = true
		origins[h] = origin{h, bh, uint(index), common.BytesToAddress(from)}
	}
	err = transactionRows.Err()
	transactionRows.Close()
	if err != nil {
		return empty, sourceError(IncompleteError, s.Name(), nil)
	}
	addresses := []string{}
	seenAddresses := map[string]bool{}
	for _, epoch := range s.options.Epochs {
		address := strings.ToLower(epoch.Address)
		if epoch.ActivationHeight <= uint64(o.ToBlock) && (epoch.RetirementHeight == nil || *epoch.RetirementHeight > uint64(o.FromBlock)) && !seenAddresses[address] {
			seenAddresses[address] = true
			addresses = append(addresses, address)
		}
	}
	sort.Strings(addresses)
	identities := map[string]bool{}
	used := 0
	for start := 0; start < len(addresses); start += 256 {
		addressBytes := [][]byte{}
		for _, address := range addresses[start:min(start+256, len(addresses))] {
			addressBytes = append(addressBytes, common.HexToAddress(address).Bytes())
		}
		rows, err := tx.Query(read, `SELECT address_hash,first_topic,second_topic,third_topic,fourth_topic,data,transaction_hash,block_hash,block_number,"index",transaction_index FROM public.logs WHERE address_hash=ANY($1::bytea[]) AND block_number BETWEEN $2 AND $3 ORDER BY block_number,transaction_index,"index"`, addressBytes, o.FromBlock, o.ToBlock)
		if err != nil {
			return empty, sourceError(InvalidError, s.Name(), nil)
		}
		for rows.Next() {
			var address, t1, t2, t3, t4, data, hash, block []byte
			var height, index, txIndex int64
			if err = rows.Scan(&address, &t1, &t2, &t3, &t4, &data, &hash, &block, &height, &index, &txIndex); err != nil || len(address) != 20 || len(hash) != 32 || len(block) != 32 || data == nil || len(data) > 64<<10 || height < o.FromBlock || height > o.ToBlock || index < 0 || txIndex < 0 || index > math.MaxUint32 || txIndex > math.MaxUint32 {
				rows.Close()
				return empty, sourceError(IncompleteError, s.Name(), nil)
			}
			topics := []common.Hash{}
			ended := false
			for _, topic := range [][]byte{t1, t2, t3, t4} {
				if topic == nil {
					ended = true
					continue
				}
				if ended || len(topic) != 32 {
					rows.Close()
					return empty, sourceError(IncompleteError, s.Name(), nil)
				}
				topics = append(topics, common.BytesToHash(topic))
			}
			log := ethtypes.Log{Address: common.BytesToAddress(address), Topics: topics, Data: append([]byte{}, data...), BlockNumber: uint64(height), BlockHash: common.BytesToHash(block), TxHash: common.BytesToHash(hash), TxIndex: uint(txIndex), Index: uint(index)}
			active := false
			for _, epoch := range s.options.Epochs {
				if strings.EqualFold(epoch.Address, log.Address.Hex()) && log.BlockNumber >= epoch.ActivationHeight && (epoch.RetirementHeight == nil || log.BlockNumber < *epoch.RetirementHeight) && (log.BlockNumber != epoch.ActivationHeight || log.TxIndex >= epoch.CreationTxIndex) {
					active = true
				}
			}
			origin, ok := origins[log.TxHash]
			key := fmt.Sprintf("%s/%s/%d", log.BlockHash, log.TxHash, log.Index)
			if !active || !ok || origin.block != log.BlockHash || origin.index != log.TxIndex || result.Blocks[log.BlockNumber].Hash != log.BlockHash || (identities[key] && !diagnostic) {
				rows.Close()
				return empty, sourceError(IntegrityError, s.Name(), nil)
			}
			identities[key] = true
			if len(log.Topics) > 0 && needsTxFrom(log.Topics[0].Hex()) {
				result.TxFromMap[strings.ToLower(log.TxHash.Hex())] = strings.ToLower(origin.from.Hex())
			}
			used += 256 + len(log.Data) + 32*len(log.Topics)
			if used > 32<<20 {
				rows.Close()
				return empty, sourceError(OversizeError, s.Name(), nil)
			}
			result.Logs = append(result.Logs, log)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return empty, sourceError(IncompleteError, s.Name(), nil)
		}
	}
	provenance := &ExplorerProvenance{SourceID: s.Name(), SchemaVersion: s.options.SchemaVersion, IndexedHeight: uint64(*indexed), ValidatedCheckpoints: []BlockRecord{}}
	samples := map[uint64]bool{uint64(o.FromBlock): true, uint64(o.ToBlock): true, uint64(o.FromBlock + (o.ToBlock-o.FromBlock)/2): true}
	heights := []uint64{}
	for height := range samples {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	for _, height := range heights {
		canonical, err := s.options.RPC.BlockAt(read, height)
		if err != nil {
			return empty, err
		}
		explorer := result.Blocks[height]
		if explorer.Hash != canonical.Hash || explorer.ParentHash != canonical.ParentHash || explorer.TimestampSeconds != canonical.TimestampSeconds || explorer.TransactionCount != canonical.TransactionCount {
			s.disagreements.Add(1)
			return empty, sourceError(IntegrityError, s.Name(), nil)
		}
		provenance.ValidatedCheckpoints = append(provenance.ValidatedCheckpoints, canonical)
	}
	if err = tx.Commit(read); err != nil {
		return empty, sourceError(IncompleteError, s.Name(), nil)
	}
	sort.Slice(result.Logs, func(i, j int) bool {
		a, b := result.Logs[i], result.Logs[j]
		if a.BlockNumber != b.BlockNumber {
			return a.BlockNumber < b.BlockNumber
		}
		if a.TxIndex != b.TxIndex {
			return a.TxIndex < b.TxIndex
		}
		return a.Index < b.Index
	})
	result.Explorer = provenance
	result.Complete = true
	result.CoveredRanges = []CoveredRange{{uint64(o.FromBlock), uint64(o.ToBlock)}}
	return result, nil
}
