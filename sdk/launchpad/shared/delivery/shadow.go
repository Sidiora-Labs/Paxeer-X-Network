package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type ShadowRecord struct {
	EventID     string `json:"eventId"`
	PayloadHash string `json:"payloadHash"`
}
type ShadowDifference struct {
	Missing, Extra, Changed, Duplicates []string
	Digest                              string
}

func CompareShadow(expected, observed []ShadowRecord) (ShadowDifference, error) {
	var d ShadowDifference
	left := map[string]string{}
	right := map[string]string{}
	for index, records := range [][]ShadowRecord{expected, observed} {
		dest := left
		if index == 1 {
			dest = right
		}
		for _, record := range records {
			if !token.MatchString(record.EventID) || !archiveHash.MatchString(record.PayloadHash) {
				return d, ErrSchema
			}
			if _, exists := dest[record.EventID]; exists {
				d.Duplicates = append(d.Duplicates, record.EventID)
			}
			dest[record.EventID] = record.PayloadHash
		}
	}
	for id, hash := range left {
		actual, ok := right[id]
		if !ok {
			d.Missing = append(d.Missing, id)
		} else if actual != hash {
			d.Changed = append(d.Changed, id)
		}
	}
	for id := range right {
		if _, ok := left[id]; !ok {
			d.Extra = append(d.Extra, id)
		}
	}
	sort.Strings(d.Missing)
	sort.Strings(d.Extra)
	sort.Strings(d.Changed)
	sort.Strings(d.Duplicates)
	sorted := append([]ShadowRecord(nil), expected...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EventID < sorted[j].EventID })
	raw, err := json.Marshal(sorted)
	if err != nil {
		return d, err
	}
	d.Digest = Hash(raw)
	if len(expected) == 0 || len(d.Missing)+len(d.Extra)+len(d.Changed)+len(d.Duplicates) > 0 {
		return d, ErrIntegrity
	}
	return d, nil
}
func (s *Store) RecordShadowTx(ctx context.Context, tx pgx.Tx, rangeDigest, path, subject string, e Envelope) error {
	if tx == nil || !archiveHash.MatchString(rangeDigest) || (path != "legacy" && path != "durable") {
		return ErrSchema
	}
	if err := e.Validate(s.policy, subject); err != nil {
		return err
	}
	if err := s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_shadow_observations")+"(range_digest,path,event_id,payload_hash,source_order) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", rangeDigest, path, e.EventID, e.PayloadHash, e.SourceOrder)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var hash string
		var oldRaw []byte
		err = tx.QueryRow(ctx, "SELECT payload_hash,source_order FROM "+s.table("delivery_shadow_observations")+" WHERE range_digest=$1 AND path=$2 AND event_id=$3", rangeDigest, path, e.EventID).Scan(&hash, &oldRaw)
		if err != nil {
			return err
		}
		var oldOrder SourceOrder
		if json.Unmarshal(oldRaw, &oldOrder) != nil || hash != e.PayloadHash || oldOrder != e.SourceOrder {
			return ErrIntegrity
		}
	}
	return s.assertEpochTx(ctx, tx)
}
func (s *Store) CompareShadowRange(ctx context.Context, manifestDigest string, subjects []string) (ShadowDifference, error) {
	var empty ShadowDifference
	if !archiveHash.MatchString(manifestDigest) || len(subjects) == 0 {
		return empty, ErrSchema
	}
	for _, subject := range subjects {
		if !s.policy.Subjects[subject] {
			return empty, ErrSchema
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, err
	}
	defer rollbackBounded(tx)
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT manifest FROM "+s.table("delivery_archive_manifests")+" WHERE manifest_digest=$1", manifestDigest).Scan(&raw); err != nil {
		return empty, err
	}
	var m ArchiveManifest
	if json.Unmarshal(raw, &m) != nil || ValidateArchiveManifest(m) != nil {
		return empty, ErrIntegrity
	}
	sets := map[string][]ShadowRecord{"legacy": {}, "durable": {}}
	rows, err := tx.Query(ctx, "SELECT path,event_id,payload_hash,source_order FROM "+s.table("delivery_shadow_observations")+" WHERE range_digest=$1 ORDER BY path,event_id", manifestDigest)
	if err != nil {
		return empty, err
	}
	for rows.Next() {
		var path string
		var record ShadowRecord
		var positionRaw []byte
		if err = rows.Scan(&path, &record.EventID, &record.PayloadHash, &positionRaw); err != nil {
			rows.Close()
			return empty, err
		}
		var order SourceOrder
		if json.Unmarshal(positionRaw, &order) != nil || order.Compare(SourceOrder{Height: strconv.FormatInt(m.FromHeight, 10), TransactionIndex: "0", LogIndex: "0"}) < 0 || order.Compare(SourceOrder{Height: strconv.FormatInt(m.ToHeight+1, 10), TransactionIndex: "0", LogIndex: "0"}) >= 0 {
			rows.Close()
			return empty, ErrIntegrity
		}
		sets[path] = append(sets[path], record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	diff, err := CompareShadow(sets["legacy"], sets["durable"])
	if err != nil {
		return diff, err
	}
	var expected []ShadowRecord
	rows, err = tx.Query(ctx, "SELECT event_id,payload_hash FROM "+s.table("delivery_outbox")+" WHERE (convert_from(envelope,'UTF8')::jsonb->'source_order'->>'height')::bigint BETWEEN $1 AND $2 AND subject=ANY($3) ORDER BY event_id", m.FromHeight, m.ToHeight, subjects)
	if err != nil {
		return diff, err
	}
	for rows.Next() {
		var record ShadowRecord
		if err = rows.Scan(&record.EventID, &record.PayloadHash); err != nil {
			rows.Close()
			return diff, err
		}
		expected = append(expected, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return diff, err
	}
	if _, err = CompareShadow(expected, sets["durable"]); err != nil {
		return diff, err
	}
	if err = s.assertEpochTx(ctx, tx); err != nil {
		return diff, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+s.table("delivery_shadow_comparisons")+"(manifest_digest,event_digest,event_count) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", manifestDigest, diff.Digest, len(expected))
	if err != nil {
		return diff, err
	}
	return diff, tx.Commit(ctx)
}
func (s *Store) BindGenerationPathTx(ctx context.Context, tx pgx.Tx, consumer, generation, path string) error {
	if tx == nil || !token.MatchString(consumer) || !token.MatchString(generation) || (path != "legacy" && path != "durable") {
		return ErrSchema
	}
	if err := s.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_generation_paths")+"(consumer_id,generation,authoritative_path) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", consumer, generation, path)
	if err != nil {
		return err
	}
	var actual string
	if err = tx.QueryRow(ctx, "SELECT authoritative_path FROM "+s.table("delivery_generation_paths")+" WHERE consumer_id=$1 AND generation=$2", consumer, generation).Scan(&actual); err != nil {
		return err
	}
	if actual != path {
		return errors.New("delivery: generation already bound to another authoritative path")
	}
	return s.assertEpochTx(ctx, tx)
}
