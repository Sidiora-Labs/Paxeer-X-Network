package delivery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/jackc/pgx/v5"
)

var archiveHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var chainHash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

type ArchiveRaw struct {
	TransactionHash, BlockHash, Emitter string
	Height                              int64
	TransactionIndex, LogIndex          uint32
	Topics                              []string
	Data                                []byte
}
type ArchiveBlock struct {
	Height       int64  `json:"height"`
	BlockHash    string `json:"blockHash"`
	RawCount     int64  `json:"rawCount"`
	SegmentHash  string `json:"segmentHash"`
	SegmentBytes int64  `json:"segmentBytes"`
}
type ArchiveManifest struct {
	Version       int            `json:"version"`
	ChainID       int            `json:"chainId"`
	SourceStream  string         `json:"sourceStream"`
	Deployment    string         `json:"deployment"`
	DecoderDigest string         `json:"decoderDigest"`
	ABIDigest     string         `json:"abiDigest"`
	CodeDigest    string         `json:"codeDigest"`
	FromHeight    int64          `json:"fromHeight"`
	ToHeight      int64          `json:"toHeight"`
	RawCount      int64          `json:"rawCount"`
	Blocks        []ArchiveBlock `json:"blocks"`
	Complete      bool           `json:"complete"`
}
type Archive struct {
	store                                          *Store
	objects                                        *storage.Client
	prefix, stream, deployment, decoder, abi, code string
}

func NewArchive(store *Store, objects *storage.Client, prefix, stream, deployment, decoder, abi, code string) (*Archive, error) {
	if store == nil || objects == nil || !token.MatchString(prefix) || !token.MatchString(stream) || !token.MatchString(deployment) || !archiveHash.MatchString(decoder) || !archiveHash.MatchString(abi) || !archiveHash.MatchString(code) {
		return nil, errors.New("delivery: actual archive providers and provenance pins required")
	}
	return &Archive{store: store, objects: objects, prefix: prefix, stream: stream, deployment: deployment, decoder: decoder, abi: abi, code: code}, nil
}
func ValidateArchiveManifest(m ArchiveManifest) error {
	if m.Version != 1 || m.ChainID != 125 || !m.Complete || !token.MatchString(m.SourceStream) || !token.MatchString(m.Deployment) || !archiveHash.MatchString(m.DecoderDigest) || !archiveHash.MatchString(m.ABIDigest) || !archiveHash.MatchString(m.CodeDigest) || m.FromHeight < 0 || m.ToHeight < m.FromHeight || m.ToHeight-m.FromHeight >= 10000 || int64(len(m.Blocks)) != m.ToHeight-m.FromHeight+1 {
		return ErrIntegrity
	}
	var count int64
	for i, b := range m.Blocks {
		if b.Height != m.FromHeight+int64(i) || !chainHash.MatchString(b.BlockHash) || b.RawCount < 0 || b.RawCount > 100000 || !archiveHash.MatchString(b.SegmentHash) || b.SegmentBytes < 0 || b.SegmentBytes > 64<<20 {
			return ErrIntegrity
		}
		count += b.RawCount
	}
	if count != m.RawCount {
		return ErrIntegrity
	}
	return nil
}
func (a *Archive) key(kind, digest string) string { return a.prefix + "/" + kind + "/" + digest }
func (a *Archive) read(ctx context.Context, kind, digest string, max int64) ([]byte, error) {
	if !archiveHash.MatchString(digest) {
		return nil, ErrIntegrity
	}
	obj, err := a.objects.Get(ctx, a.key(kind, digest))
	if err != nil {
		return nil, errors.New("delivery: archive object unavailable")
	}
	defer obj.Body.Close()
	if obj.Size > max {
		return nil, ErrIntegrity
	}
	raw, err := io.ReadAll(io.LimitReader(obj.Body, max+1))
	if err != nil || int64(len(raw)) > max || Hash(raw) != digest {
		return nil, ErrIntegrity
	}
	return raw, nil
}
func (a *Archive) put(ctx context.Context, kind string, raw []byte) (string, error) {
	digest := Hash(raw)
	if err := a.objects.Put(ctx, a.key(kind, digest), bytes.NewReader(raw), int64(len(raw)), "application/octet-stream"); err != nil {
		return "", errors.New("delivery: archive write unavailable")
	}
	stored, err := a.read(ctx, kind, digest, int64(len(raw)))
	if err != nil || !bytes.Equal(stored, raw) {
		return "", ErrIntegrity
	}
	return digest, nil
}
func (a *Archive) sourceBlock(ctx context.Context, tx pgx.Tx, height int64) (ArchiveBlock, []byte, error) {
	var block ArchiveBlock
	block.Height = height
	err := tx.QueryRow(ctx, `SELECT block_hash,raw_count FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete`, a.stream, height).Scan(&block.BlockHash, &block.RawCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return block, nil, ErrGap
	}
	if err != nil {
		return block, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT transaction_hash,block_hash,emitter,height,transaction_index,log_index,topics,data FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND height=$1 ORDER BY transaction_index,log_index`, height)
	if err != nil {
		return block, nil, err
	}
	defer rows.Close()
	var raw bytes.Buffer
	var count int64
	var previous SourceOrder
	for rows.Next() {
		var r ArchiveRaw
		var topics []byte
		if err = rows.Scan(&r.TransactionHash, &r.BlockHash, &r.Emitter, &r.Height, &r.TransactionIndex, &r.LogIndex, &topics, &r.Data); err != nil {
			return block, nil, err
		}
		if err = json.Unmarshal(topics, &r.Topics); err != nil {
			return block, nil, ErrIntegrity
		}
		order := SourceOrder{strconv.FormatInt(r.Height, 10), strconv.FormatUint(uint64(r.TransactionIndex), 10), strconv.FormatUint(uint64(r.LogIndex), 10)}
		if r.BlockHash != block.BlockHash || !chainHash.MatchString(r.TransactionHash) || count > 0 && order.Compare(previous) <= 0 {
			return block, nil, ErrIntegrity
		}
		previous = order
		line, err := json.Marshal(r)
		if err != nil {
			return block, nil, err
		}
		if raw.Len()+len(line)+1 > 64<<20 {
			return block, nil, errors.New("delivery: archive block exceeds bounded segment")
		}
		raw.Write(line)
		raw.WriteByte('\n')
		count++
		if count > 100000 {
			return block, nil, ErrIntegrity
		}
	}
	if err = rows.Err(); err != nil {
		return block, nil, err
	}
	if count != block.RawCount {
		return block, nil, ErrGap
	}
	block.SegmentBytes = int64(raw.Len())
	block.SegmentHash = Hash(raw.Bytes())
	return block, raw.Bytes(), nil
}
func (a *Archive) Capture(ctx context.Context, from, to int64) (ArchiveManifest, string, error) {
	m := ArchiveManifest{Version: 1, ChainID: 125, SourceStream: a.stream, Deployment: a.deployment, DecoderDigest: a.decoder, ABIDigest: a.abi, CodeDigest: a.code, FromHeight: from, ToHeight: to, Complete: true}
	if from < 0 || to < from || to-from >= 10000 {
		return m, "", ErrSchema
	}
	tx, err := a.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return m, "", err
	}
	defer rollbackBounded(tx)
	if err = a.store.assertEpochTx(ctx, tx); err != nil {
		return m, "", err
	}
	for height := from; height <= to; height++ {
		block, raw, err := a.sourceBlock(ctx, tx, height)
		if err != nil {
			return m, "", err
		}
		if _, err = a.put(ctx, "segments", raw); err != nil {
			return m, "", err
		}
		m.Blocks = append(m.Blocks, block)
		m.RawCount += block.RawCount
	}
	if err = ValidateArchiveManifest(m); err != nil {
		return m, "", err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m, "", err
	}
	digest, err := a.put(ctx, "manifests", raw)
	if err != nil {
		return m, "", err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO "+a.store.table("delivery_archive_manifests")+"(manifest_digest,source_stream,from_height,to_height,raw_count,final_block_hash,manifest) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING", digest, a.stream, from, to, m.RawCount, m.Blocks[len(m.Blocks)-1].BlockHash, raw)
	if err != nil {
		return m, "", err
	}
	if tag.RowsAffected() == 0 {
		var old []byte
		if err = tx.QueryRow(ctx, "SELECT manifest FROM "+a.store.table("delivery_archive_manifests")+" WHERE manifest_digest=$1", digest).Scan(&old); err != nil {
			return m, "", err
		}
		if !bytes.Equal(old, raw) {
			return m, "", ErrIntegrity
		}
	}
	if err = a.store.assertEpochTx(ctx, tx); err != nil {
		return m, "", err
	}
	return m, digest, tx.Commit(ctx)
}
func (a *Archive) Load(ctx context.Context, digest string) (ArchiveManifest, error) {
	var m ArchiveManifest
	raw, err := a.read(ctx, "manifests", digest, 8<<20)
	if err != nil {
		return m, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&m); err != nil {
		return m, ErrSchema
	}
	if err = ValidateArchiveManifest(m); err != nil {
		return m, err
	}
	if m.SourceStream != a.stream || m.Deployment != a.deployment || m.DecoderDigest != a.decoder || m.ABIDigest != a.abi || m.CodeDigest != a.code {
		return m, ErrIntegrity
	}
	var stored []byte
	err = a.store.pool.QueryRow(ctx, "SELECT manifest FROM "+a.store.table("delivery_archive_manifests")+" WHERE manifest_digest=$1", digest).Scan(&stored)
	if err != nil {
		return m, err
	}
	if !bytes.Equal(stored, raw) {
		return m, ErrIntegrity
	}
	return m, nil
}
func (a *Archive) records(ctx context.Context, block ArchiveBlock) ([]ArchiveRaw, error) {
	raw, err := a.read(ctx, "segments", block.SegmentHash, 64<<20)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != block.SegmentBytes {
		return nil, ErrIntegrity
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), MaxEnvelopeBytes)
	out := []ArchiveRaw{}
	for scanner.Scan() {
		var r ArchiveRaw
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&r); err != nil {
			return nil, ErrSchema
		}
		if r.Height != block.Height || r.BlockHash != block.BlockHash {
			return nil, ErrIntegrity
		}
		out = append(out, r)
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	if int64(len(out)) != block.RawCount {
		return nil, ErrGap
	}
	return out, nil
}

func (a *Archive) ReadBlock(ctx context.Context, manifestDigest string, height int64) (ArchiveBlock, []ArchiveRaw, error) {
	m, err := a.Load(ctx, manifestDigest)
	if err != nil {
		return ArchiveBlock{}, nil, err
	}
	if height < m.FromHeight || height > m.ToHeight {
		return ArchiveBlock{}, nil, ErrGap
	}
	block := m.Blocks[height-m.FromHeight]
	records, err := a.records(ctx, block)
	return block, records, err
}
func (a *Archive) ValidateSourceTx(ctx context.Context, tx pgx.Tx, m ArchiveManifest) error {
	if ValidateArchiveManifest(m) != nil || m.SourceStream != a.stream {
		return ErrIntegrity
	}
	for _, block := range m.Blocks {
		actual, raw, err := a.sourceBlock(ctx, tx, block.Height)
		if err != nil {
			return err
		}
		if actual.BlockHash != block.BlockHash || actual.RawCount != block.RawCount || Hash(raw) != block.SegmentHash {
			return ErrIntegrity
		}
	}
	return nil
}
