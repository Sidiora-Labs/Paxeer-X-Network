package rehearsal

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	EnvSourceURL = "CEREMONY_SOURCE_DATABASE_URL"
	LedgerTable  = "_migrations"
)

var (
	ErrWritableRole    = errors.New("rehearsal: source role can write")
	ErrSource          = errors.New("rehearsal: source database read failed")
	ErrTarget          = errors.New("rehearsal: target database write failed")
	ErrTargetNotEmpty  = errors.New("rehearsal: target database already holds tables")
	ErrNoLedger        = errors.New("rehearsal: source database has no migration ledger")
	ErrNoPrimaryKey    = errors.New("rehearsal: source table has no primary key")
	ErrCountMismatch   = errors.New("rehearsal: copied row counts differ")
	ErrDigestMismatch  = errors.New("rehearsal: copied stream digests differ")
	ErrForeignKeyCycle = errors.New("rehearsal: foreign keys between copied tables form a cycle")
	errCatalog         = errors.New("rehearsal: catalog read failed")
)

var sessionSettings = []string{
	"set TimeZone = 'UTC'",
	"set DateStyle = 'ISO, MDY'",
	"set IntervalStyle = 'postgres'",
	"set bytea_output = 'hex'",
	"set client_encoding = 'UTF8'",
}

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Column struct {
	Name       string
	Type       string
	Collatable bool
}

type Sequence struct {
	Column    string
	LastValue int64
	IsCalled  bool
}

type Table struct {
	Name       string
	Columns    []Column
	PrimaryKey []string
	Sequences  []Sequence
}

func ident(name string) string { return pgx.Identifier{name}.Sanitize() }

func (t Table) Ident() string { return ident(t.Name) }

func (t Table) ColumnList(alias string) string {
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = qualify(alias, c.Name)
	}
	return strings.Join(cols, ", ")
}

func (t Table) OrderBy(alias string) string {
	coll := make(map[string]bool, len(t.Columns))
	for _, c := range t.Columns {
		coll[c.Name] = c.Collatable
	}
	keys := make([]string, len(t.PrimaryKey))
	for i, k := range t.PrimaryKey {
		keys[i] = qualify(alias, k)
		if coll[k] {
			keys[i] += ` collate "C"`
		}
	}
	return strings.Join(keys, ", ")
}

func (t Table) KeyMatch(a, b string) string {
	parts := make([]string, len(t.PrimaryKey))
	for i, k := range t.PrimaryKey {
		parts[i] = qualify(a, k) + " = " + qualify(b, k)
	}
	return strings.Join(parts, " and ")
}

func (t Table) SelectOrdered(from, alias string) string {
	return "select " + t.ColumnList(alias) + " from " + from + " " + alias + " order by " + t.OrderBy(alias)
}

func (t Table) CopyOut(from, alias string) string {
	return "copy (" + t.SelectOrdered(from, alias) + ") to stdout"
}

func (t Table) CopyIn(into string) string {
	return "copy " + into + " (" + t.ColumnList("") + ") from stdin"
}

func qualify(alias, col string) string {
	if alias == "" {
		return ident(col)
	}
	return alias + "." + ident(col)
}

type Source struct {
	conn   *pgx.Conn
	Tables []Table
	Ledger []string
}

func Connect(ctx context.Context, url string) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	for _, s := range sessionSettings {
		if _, err := conn.Exec(ctx, s); err != nil {
			conn.Close(context.Background())
			return nil, err
		}
	}
	return conn, nil
}

func OpenSource(ctx context.Context, url string) (*Source, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrConfig, EnvSourceURL)
	}
	conn, err := Connect(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("%w: connect: %v", ErrSource, err)
	}
	s := &Source{conn: conn}
	if err := s.open(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Source) open(ctx context.Context) error {
	if err := checkReadOnlyRole(ctx, s.conn); err != nil {
		return err
	}
	if _, err := s.conn.Exec(ctx, "begin isolation level repeatable read"); err != nil {
		return fmt.Errorf("%w: begin: %v", ErrSource, err)
	}
	if _, err := s.conn.Exec(ctx, "set transaction read only"); err != nil {
		return fmt.Errorf("%w: set transaction read only: %v", ErrSource, err)
	}
	var readOnly, isolation string
	if err := s.conn.QueryRow(ctx, "select current_setting('transaction_read_only'), current_setting('transaction_isolation')").Scan(&readOnly, &isolation); err != nil {
		return fmt.Errorf("%w: inspect transaction: %v", ErrSource, err)
	}
	if readOnly != "on" || isolation != "repeatable read" {
		return fmt.Errorf("%w: transaction is %s and read only %s", ErrWritableRole, isolation, readOnly)
	}
	tables, err := ReadTables(ctx, s.conn)
	if err != nil {
		return err
	}
	hasLedger := false
	for i := range tables {
		if tables[i].Name == LedgerTable {
			hasLedger = true
		}
		if err := readSequences(ctx, s.conn, &tables[i]); err != nil {
			return err
		}
	}
	if !hasLedger {
		return ErrNoLedger
	}
	s.Tables = tables
	rows, err := s.conn.Query(ctx, "select filename from "+ident(LedgerTable)+` order by filename collate "C"`)
	if err != nil {
		return fmt.Errorf("%w: read ledger: %v", ErrSource, err)
	}
	s.Ledger, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("%w: read ledger: %v", ErrSource, err)
	}
	return nil
}

func (s *Source) Close() {
	if s.conn == nil {
		return
	}
	ctx := context.Background()
	_, _ = s.conn.Exec(ctx, "rollback")
	_ = s.conn.Close(ctx)
	s.conn = nil
}

func (s *Source) Counts(ctx context.Context) (map[string]int64, error) {
	return TableCounts(ctx, s.conn, s.Tables)
}

func (s *Source) Stream(ctx context.Context, t Table, w io.Writer) (int64, error) {
	tag, err := s.conn.PgConn().CopyTo(ctx, w, t.CopyOut(t.Ident(), "s"))
	if err != nil {
		return 0, fmt.Errorf("%w: copy %s: %v", ErrSource, t.Name, err)
	}
	return tag.RowsAffected(), nil
}

func checkReadOnlyRole(ctx context.Context, conn *pgx.Conn) error {
	var super bool
	if err := conn.QueryRow(ctx, "select rolsuper from pg_roles where rolname = current_user").Scan(&super); err != nil {
		return fmt.Errorf("%w: inspect role: %v", ErrSource, err)
	}
	if super {
		return fmt.Errorf("%w: the role is a superuser", ErrWritableRole)
	}
	rows, err := conn.Query(ctx, `select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = current_schema() and c.relkind in ('r', 'p')
		  and has_table_privilege(c.oid, 'INSERT, UPDATE, DELETE, TRUNCATE')
		order by c.relname collate "C"`)
	if err != nil {
		return fmt.Errorf("%w: inspect privileges: %v", ErrSource, err)
	}
	writable, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("%w: inspect privileges: %v", ErrSource, err)
	}
	if len(writable) > 0 {
		return fmt.Errorf("%w: write privilege on %s", ErrWritableRole, strings.Join(writable, ", "))
	}
	if _, err := conn.Exec(ctx, "set default_transaction_read_only = on"); err != nil {
		return fmt.Errorf("%w: default_transaction_read_only cannot be forced on: %v", ErrWritableRole, err)
	}
	var setting string
	if err := conn.QueryRow(ctx, "select current_setting('default_transaction_read_only')").Scan(&setting); err != nil {
		return fmt.Errorf("%w: inspect session: %v", ErrSource, err)
	}
	if setting != "on" {
		return fmt.Errorf("%w: default_transaction_read_only is %s", ErrWritableRole, setting)
	}
	return nil
}

func ReadTables(ctx context.Context, q Querier) ([]Table, error) {
	rows, err := q.Query(ctx, `select c.oid::bigint, c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = current_schema() and c.relkind in ('r', 'p') and not c.relispartition
		order by c.relname collate "C"`)
	if err != nil {
		return nil, fmt.Errorf("%w: list tables: %v", errCatalog, err)
	}
	type rel struct {
		oid  int64
		name string
	}
	var rels []rel
	for rows.Next() {
		var r rel
		if err := rows.Scan(&r.oid, &r.name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: list tables: %v", errCatalog, err)
		}
		rels = append(rels, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list tables: %v", errCatalog, err)
	}
	tables := make([]Table, 0, len(rels))
	for _, r := range rels {
		t := Table{Name: r.name}
		crow, err := q.Query(ctx, `select attname, format_type(atttypid, atttypmod), attcollation <> 0
			from pg_attribute where attrelid = $1::bigint::oid and attnum > 0 and not attisdropped and attgenerated = ''
			order by attnum`, r.oid)
		if err != nil {
			return nil, fmt.Errorf("%w: columns of %s: %v", errCatalog, r.name, err)
		}
		for crow.Next() {
			var c Column
			if err := crow.Scan(&c.Name, &c.Type, &c.Collatable); err != nil {
				crow.Close()
				return nil, fmt.Errorf("%w: columns of %s: %v", errCatalog, r.name, err)
			}
			t.Columns = append(t.Columns, c)
		}
		crow.Close()
		if err := crow.Err(); err != nil {
			return nil, fmt.Errorf("%w: columns of %s: %v", errCatalog, r.name, err)
		}
		krow, err := q.Query(ctx, `select a.attname from pg_index i
			join lateral unnest(i.indkey) with ordinality as k(attnum, ord) on true
			join pg_attribute a on a.attrelid = i.indrelid and a.attnum = k.attnum
			where i.indrelid = $1::bigint::oid and i.indisprimary order by k.ord`, r.oid)
		if err != nil {
			return nil, fmt.Errorf("%w: primary key of %s: %v", errCatalog, r.name, err)
		}
		t.PrimaryKey, err = pgx.CollectRows(krow, pgx.RowTo[string])
		if err != nil {
			return nil, fmt.Errorf("%w: primary key of %s: %v", errCatalog, r.name, err)
		}
		if len(t.PrimaryKey) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoPrimaryKey, r.name)
		}
		tables = append(tables, t)
	}
	return tables, nil
}

func readSequences(ctx context.Context, q Querier, t *Table) error {
	for _, c := range t.Columns {
		var seq *string
		if err := q.QueryRow(ctx, "select pg_get_serial_sequence($1, $2)", t.Ident(), c.Name).Scan(&seq); err != nil {
			return fmt.Errorf("%w: sequence of %s.%s: %v", ErrSource, t.Name, c.Name, err)
		}
		if seq == nil {
			continue
		}
		s := Sequence{Column: c.Name}
		if err := q.QueryRow(ctx, "select last_value, is_called from "+*seq).Scan(&s.LastValue, &s.IsCalled); err != nil {
			return fmt.Errorf("%w: sequence of %s.%s: %v", ErrSource, t.Name, c.Name, err)
		}
		t.Sequences = append(t.Sequences, s)
	}
	return nil
}

func TableCounts(ctx context.Context, q Querier, tables []Table) (map[string]int64, error) {
	out := make(map[string]int64, len(tables))
	for _, t := range tables {
		var n int64
		if err := q.QueryRow(ctx, "select count(*) from "+t.Ident()).Scan(&n); err != nil {
			return nil, fmt.Errorf("rehearsal: count %s: %w", t.Name, err)
		}
		out[t.Name] = n
	}
	return out, nil
}

func Digest(ctx context.Context, conn *pgconn.PgConn, copyOut string) ([32]byte, int64, error) {
	h := sha256.New()
	tag, err := conn.CopyTo(ctx, h, copyOut)
	if err != nil {
		return [32]byte{}, 0, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, tag.RowsAffected(), nil
}

func CheckEmptyTarget(ctx context.Context, q Querier) error {
	rows, err := q.Query(ctx, `select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = current_schema() and c.relkind in ('r', 'p', 'v', 'm', 'f')
		order by c.relname collate "C"`)
	if err != nil {
		return fmt.Errorf("%w: inspect target: %v", ErrTarget, err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("%w: inspect target: %v", ErrTarget, err)
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: %d relations, first %s", ErrTargetNotEmpty, len(names), names[0])
	}
	return nil
}

func ForeignKeyOrder(ctx context.Context, q Querier, tables []Table) ([]Table, error) {
	byName := make(map[string]Table, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}
	rows, err := q.Query(ctx, `select c.relname, r.relname from pg_constraint k
		join pg_class c on c.oid = k.conrelid join pg_class r on r.oid = k.confrelid
		join pg_namespace n on n.oid = c.relnamespace
		where k.contype = 'f' and n.nspname = current_schema()`)
	if err != nil {
		return nil, fmt.Errorf("%w: foreign keys: %v", ErrTarget, err)
	}
	parents := make(map[string]map[string]bool)
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: foreign keys: %v", ErrTarget, err)
		}
		if child == parent {
			continue
		}
		if _, ok := byName[child]; !ok {
			continue
		}
		if _, ok := byName[parent]; !ok {
			continue
		}
		if parents[child] == nil {
			parents[child] = make(map[string]bool)
		}
		parents[child][parent] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: foreign keys: %v", ErrTarget, err)
	}
	done := make(map[string]bool, len(tables))
	var order []Table
	for len(order) < len(tables) {
		var ready []string
		for name := range byName {
			if done[name] {
				continue
			}
			free := true
			for p := range parents[name] {
				if !done[p] {
					free = false
					break
				}
			}
			if free {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			return nil, ErrForeignKeyCycle
		}
		sort.Strings(ready)
		for _, name := range ready {
			done[name] = true
			order = append(order, byName[name])
		}
	}
	return order, nil
}

func CheckCounterparts(ctx context.Context, q Querier, tables []Table) error {
	target, err := ReadTables(ctx, q)
	if err != nil {
		return err
	}
	have := make(map[string]map[string]string, len(target))
	for _, t := range target {
		cols := make(map[string]string, len(t.Columns))
		for _, c := range t.Columns {
			cols[c.Name] = c.Type
		}
		have[t.Name] = cols
	}
	var missing []string
	for _, t := range tables {
		cols, ok := have[t.Name]
		if !ok {
			missing = append(missing, t.Name)
			continue
		}
		for _, c := range t.Columns {
			typ, ok := cols[c.Name]
			if !ok {
				missing = append(missing, t.Name+"."+c.Name)
			} else if typ != c.Type {
				missing = append(missing, t.Name+"."+c.Name+" ("+c.Type+" became "+typ+")")
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: no counterpart for %s", ErrSchema, strings.Join(missing, ", "))
	}
	return nil
}

func SetTriggers(ctx context.Context, q Querier, tables []Table, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	for _, t := range tables {
		if _, err := q.Exec(ctx, "alter table "+t.Ident()+" "+verb+" trigger user"); err != nil {
			return fmt.Errorf("%w: %s triggers on %s: %v", ErrTarget, verb, t.Name, err)
		}
	}
	return nil
}

func SetSequences(ctx context.Context, q Querier, tables []Table) error {
	for _, t := range tables {
		for _, s := range t.Sequences {
			var seq *string
			if err := q.QueryRow(ctx, "select pg_get_serial_sequence($1, $2)", t.Ident(), s.Column).Scan(&seq); err != nil {
				return fmt.Errorf("%w: sequence of %s.%s: %v", ErrTarget, t.Name, s.Column, err)
			}
			if seq == nil {
				return fmt.Errorf("%w: no counterpart for the sequence of %s.%s", ErrSchema, t.Name, s.Column)
			}
			source := s.LastValue
			if !s.IsCalled {
				source--
			}
			var current, max int64
			var called bool
			if err := q.QueryRow(ctx, "select last_value, is_called from "+*seq).Scan(&current, &called); err != nil {
				return fmt.Errorf("%w: sequence of %s.%s: %v", ErrTarget, t.Name, s.Column, err)
			}
			if !called {
				current--
			}
			if err := q.QueryRow(ctx, "select coalesce(max("+ident(s.Column)+"), 0)::bigint from "+t.Ident()).Scan(&max); err != nil {
				return fmt.Errorf("%w: sequence of %s.%s: %v", ErrTarget, t.Name, s.Column, err)
			}
			v := source
			if current > v {
				v = current
			}
			if max > v {
				v = max
			}
			var err error
			if v < 1 {
				_, err = q.Exec(ctx, "select setval($1::regclass, 1, false)", *seq)
			} else {
				_, err = q.Exec(ctx, "select setval($1::regclass, $2, true)", *seq, v)
			}
			if err != nil {
				return fmt.Errorf("%w: sequence of %s.%s: %v", ErrTarget, t.Name, s.Column, err)
			}
		}
	}
	return nil
}

type TableCopy struct {
	Table        string
	SourceRows   int64
	TargetRows   int64
	SourceDigest [32]byte
	TargetDigest [32]byte
}

type CopyReport struct {
	Tables       []TableCopy
	LedgerBefore int
	AppliedAfter []string
}

func (r CopyReport) Rows() int64 {
	var n int64
	for _, t := range r.Tables {
		n += t.SourceRows
	}
	return n
}

func (r CopyReport) Verify() error {
	var counts, digests []string
	for _, t := range r.Tables {
		if t.SourceRows != t.TargetRows {
			counts = append(counts, fmt.Sprintf("%s source %d target %d", t.Table, t.SourceRows, t.TargetRows))
		}
		if t.SourceDigest != t.TargetDigest {
			digests = append(digests, t.Table)
		}
	}
	if len(counts) > 0 {
		return fmt.Errorf("%w: %s", ErrCountMismatch, strings.Join(counts, ", "))
	}
	if len(digests) > 0 {
		return fmt.Errorf("%w: %s", ErrDigestMismatch, strings.Join(digests, ", "))
	}
	return nil
}

func (r CopyReport) CountLines() []string {
	out := make([]string, len(r.Tables))
	for i, t := range r.Tables {
		out[i] = fmt.Sprintf("table=%s source_rows=%d target_rows=%d", t.Table, t.SourceRows, t.TargetRows)
	}
	return out
}

func (r CopyReport) DigestLines() []string {
	out := make([]string, len(r.Tables))
	for i, t := range r.Tables {
		out[i] = fmt.Sprintf("table=%s source_rows=%d target_rows=%d source_sha256=%x target_sha256=%x", t.Table, t.SourceRows, t.TargetRows, t.SourceDigest, t.TargetDigest)
	}
	return out
}

func (r CopyReport) Summary() string {
	return fmt.Sprintf("tables=%d rows=%d ledger_before=%d applied_after=%d", len(r.Tables), r.Rows(), r.LedgerBefore, len(r.AppliedAfter))
}

func CopySource(ctx context.Context, sourceURL, targetURL, migrationsDir string) (CopyReport, error) {
	var report CopyReport
	m, err := LoadMigrations(migrationsDir)
	if err != nil {
		return report, err
	}
	src, err := OpenSource(ctx, sourceURL)
	if err != nil {
		return report, err
	}
	defer src.Close()
	if strings.TrimSpace(targetURL) == "" {
		return report, fmt.Errorf("%w: target connection URL is not set", ErrConfig)
	}
	tgt, err := Connect(ctx, targetURL)
	if err != nil {
		return report, fmt.Errorf("%w: connect: %v", ErrTarget, err)
	}
	defer tgt.Close(context.Background())
	if err := CheckEmptyTarget(ctx, tgt); err != nil {
		return report, err
	}
	report.LedgerBefore = len(src.Ledger)
	report.AppliedAfter, err = PrepareSchema(ctx, tgt, m, src.Ledger, src.Tables, func(ctx context.Context) error {
		report.Tables, err = load(ctx, src, tgt)
		return err
	})
	return report, err
}

func load(ctx context.Context, src *Source, tgt *pgx.Conn) ([]TableCopy, error) {
	order, err := ForeignKeyOrder(ctx, tgt, src.Tables)
	if err != nil {
		return nil, err
	}
	counts, err := src.Counts(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSource, err)
	}
	tx, err := tgt.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: begin: %v", ErrTarget, err)
	}
	defer tx.Rollback(context.Background())
	if err := SetTriggers(ctx, tx, order, false); err != nil {
		return nil, err
	}
	names := make([]string, len(order))
	for i, t := range order {
		names[i] = t.Ident()
	}
	if _, err := tx.Exec(ctx, "truncate table "+strings.Join(names, ", ")); err != nil {
		return nil, fmt.Errorf("%w: truncate: %v", ErrTarget, err)
	}
	copies := make([]TableCopy, len(order))
	loaded := make([]int64, len(order))
	for i, t := range order {
		copies[i].Table = t.Name
		streamed, in, digest, err := Stream(ctx, src, tx.Conn().PgConn(), t, t.CopyIn(t.Ident()))
		if err != nil {
			return nil, err
		}
		if streamed != counts[t.Name] || in != streamed {
			return nil, fmt.Errorf("%w: %s counted %d, streamed %d, loaded %d", ErrCountMismatch, t.Name, counts[t.Name], streamed, in)
		}
		copies[i].SourceRows, copies[i].SourceDigest, loaded[i] = counts[t.Name], digest, in
	}
	if err := SetSequences(ctx, tx, order); err != nil {
		return nil, err
	}
	after, err := TableCounts(ctx, tx, order)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTarget, err)
	}
	for i, t := range order {
		digest, n, err := Digest(ctx, tx.Conn().PgConn(), t.CopyOut(t.Ident(), "t"))
		if err != nil {
			return nil, fmt.Errorf("%w: re-read %s: %v", ErrTarget, t.Name, err)
		}
		if n != after[t.Name] {
			return nil, fmt.Errorf("%w: %s counted %d, re-read %d", ErrCountMismatch, t.Name, after[t.Name], n)
		}
		copies[i].TargetRows, copies[i].TargetDigest = after[t.Name], digest
	}
	report := CopyReport{Tables: copies}
	if err := report.Verify(); err != nil {
		return copies, err
	}
	if err := SetTriggers(ctx, tx, order, true); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%w: commit: %v", ErrTarget, err)
	}
	return copies, nil
}

func Stream(ctx context.Context, src *Source, dst *pgconn.PgConn, t Table, copyIn string) (streamed, loaded int64, digest [32]byte, err error) {
	pr, pw := io.Pipe()
	h := sha256.New()
	type result struct {
		n   int64
		err error
	}
	out := make(chan result, 1)
	go func() {
		n, err := src.Stream(ctx, t, io.MultiWriter(pw, h))
		pw.CloseWithError(err)
		out <- result{n, err}
	}()
	tag, inErr := dst.CopyFrom(ctx, pr, copyIn)
	if inErr != nil {
		pr.CloseWithError(inErr)
	}
	res := <-out
	if res.err != nil {
		return 0, 0, digest, res.err
	}
	if inErr != nil {
		return 0, 0, digest, fmt.Errorf("%w: load %s: %v", ErrTarget, t.Name, inErr)
	}
	copy(digest[:], h.Sum(nil))
	return res.n, tag.RowsAffected(), digest, nil
}
