package fanout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCommitUncertain  = errors.New("message commit outcome uncertain")
	ErrUnavailable      = errors.New("chat prerequisite unavailable")
	ErrNotFound         = errors.New("conversation not found")
	ErrConflict         = errors.New("client_message_id or revision conflict")
	ErrCursor           = errors.New("invalid replay cursor")
	ErrReplayExpired    = errors.New("replay_window_expired")
	ErrSnapshotRequired = errors.New("history_snapshot_required")
	ErrDeleted          = errors.New("message deleted")
	ErrInvalid          = errors.New("invalid chat input")
)

// SendPolicy must lock/read current block, ban, membership and pool existence
// policy through this transaction. Missing policy fails closed; the moderator
// adapter is supplied by L38, not inferred from client claims.
type SendPolicy func(context.Context, pgx.Tx, SendInput) error

type DurableStore struct {
	Projection         func(context.Context, DurableMessage) (DurableMessage, error)
	ReadPolicy         func(context.Context, pgx.Tx, string, string) error
	PendingOutboxLimit int
	MaxOutboxAge       time.Duration
	ChannelNamespace   string
	Pool               *pgxpool.Pool
	NotificationQueue  func(context.Context, pgx.Tx, DurableMessage) error
	Policy             SendPolicy
	CursorKey          []byte
	Clock              func() time.Time
}
type SendInput struct{ Actor, Kind, Target, ClientID, Content, ReplyTo string }
type DurableMessage struct {
	PoolAddress    string `json:"pool_address,omitempty"`
	Type           string `json:"type"`
	ID             string `json:"id"`
	ClientID       string `json:"client_message_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	Sender         string `json:"sender"`
	Content        string `json:"content"`
	ReplyTo        string `json:"reply_to_id,omitempty"`
	Sequence       int64  `json:"sequence,string"`
	Revision       int64  `json:"revision,string"`
	CreatedAt      int64  `json:"created_at"`
	Deleted        bool   `json:"deleted"`
}

func (s *DurableStore) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *DurableStore) ready() bool { return s != nil && s.Pool != nil }
func conversation(in SendInput) string {
	if in.Kind == "pool" {
		return "pool:" + in.Target
	}
	a, b := common.SortedPair(in.Actor, in.Target)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(a+":"+b)).String()
}

const messageColumns = `id,conversation_id,sender,content,reply_to,sequence,revision,extract(epoch from created_at)::bigint,deleted_at IS NOT NULL,client_id`

func scanMessage(row pgx.Row, kind string) (m DurableMessage, err error) {
	err = row.Scan(&m.ID, &m.ConversationID, &m.Sender, &m.Content, &m.ReplyTo, &m.Sequence, &m.Revision, &m.CreatedAt, &m.Deleted, &m.ClientID)
	m.Type = wireKind(kind)
	if kind == "pool" {
		m.PoolAddress = strings.TrimPrefix(m.ConversationID, "pool:")
	}
	if m.Deleted {
		m.Content = ""
		m.ReplyTo = ""
	}
	return
}

// Send returns only after message, membership, sequence and publication intent
// commit together. A retry resolves an uncertain commit using the same client ID.
func (s *DurableStore) Send(ctx context.Context, in SendInput) (DurableMessage, error) {
	var zero DurableMessage
	if !s.ready() || s.Policy == nil {
		return zero, ErrUnavailable
	}
	in.Actor = common.NormalizeAddr(in.Actor)
	in.Target = common.NormalizeAddr(in.Target)
	if len(in.Content) > 16384 || !utf8.ValidString(in.Content) {
		return zero, ErrInvalid
	}
	in.Content = common.Sanitize(in.Content)
	if !common.IsAddr(in.Actor) || !common.IsAddr(in.Target) || in.Actor == in.Target || (in.Kind != "dm" && in.Kind != "pool") || len(in.ClientID) < 1 || len(in.ClientID) > 128 || !utf8.ValidString(in.Content) || utf8.RuneCountInString(in.Content) < 1 || utf8.RuneCountInString(in.Content) > 2000 || len(in.ReplyTo) > 128 {
		return zero, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer rollback(tx)
	if err = s.lockReserve(ctx, tx); err != nil {
		return zero, err
	}
	// A sender-wide lock serializes idempotency even when a retry changes target.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,36))`, in.Actor); err != nil {
		return zero, err
	}
	if err = s.Policy(ctx, tx, in); err != nil {
		return zero, err
	}
	conv := conversation(in)
	previous, e := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM chat_messages WHERE sender=$1 AND client_id=$2`, in.Actor, in.ClientID), in.Kind)
	if e == nil {
		var member string
		if err = tx.QueryRow(ctx, `SELECT wallet FROM chat_members WHERE conversation_id=$1 AND wallet=$2 FOR SHARE`, previous.ConversationID, in.Actor).Scan(&member); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return zero, ErrNotFound
			}
			return zero, err
		}
		// Compare immutable creation content even after later edits via outbox v1.
		var original DurableMessage
		var payload []byte
		if err = tx.QueryRow(ctx, `SELECT payload FROM chat_outbox WHERE message_id=$1 AND revision=1`, previous.ID).Scan(&payload); err != nil {
			return zero, err
		}
		if err = json.Unmarshal(payload, &original); err != nil {
			return zero, err
		}
		if original.ConversationID != conv || original.Content != in.Content || original.ReplyTo != in.ReplyTo {
			return zero, ErrConflict
		}
		return previous, commitMessage(ctx, tx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return zero, e
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return zero, err
	}
	var inserted string
	err = tx.QueryRow(ctx, `INSERT INTO chat_conversations(id,kind) VALUES($1,$2) ON CONFLICT(id) DO NOTHING RETURNING id`, conv, in.Kind).Scan(&inserted)
	created := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	if in.Kind == "dm" && created {
		if _, err = tx.Exec(ctx, `INSERT INTO chat_members(conversation_id,wallet) VALUES($1,$2),($1,$3)`, conv, in.Actor, in.Target); err != nil {
			return zero, err
		}
	} else {
		wallets := []string{in.Actor}
		if in.Kind == "dm" {
			wallets = append(wallets, in.Target)
		}
		for _, wallet := range wallets {
			var member string
			if err = tx.QueryRow(ctx, `SELECT wallet FROM chat_members WHERE conversation_id=$1 AND wallet=$2 FOR SHARE`, conv, wallet).Scan(&member); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return zero, ErrNotFound
				}
				return zero, err
			}
		}
	}

	if in.ReplyTo != "" {
		var reply string
		if err = tx.QueryRow(ctx, `SELECT id FROM chat_messages WHERE id=$1 AND conversation_id=$2 AND deleted_at IS NULL`, in.ReplyTo, conv).Scan(&reply); err != nil {
			return zero, ErrNotFound
		}
	}
	var seq int64
	if err = tx.QueryRow(ctx, `UPDATE chat_conversations SET next_sequence=next_sequence+1 WHERE id=$1 RETURNING next_sequence`, conv).Scan(&seq); err != nil {
		return zero, err
	}
	id, e := uuid.NewV7()
	if e != nil {
		return zero, e
	}
	now := s.now()
	m := DurableMessage{Type: wireKind(in.Kind), ID: id.String(), ClientID: in.ClientID, ConversationID: conv, Sender: in.Actor, Content: in.Content, ReplyTo: in.ReplyTo, Sequence: seq, Revision: 1, CreatedAt: now.Unix()}
	if in.Kind == "pool" {
		m.PoolAddress = in.Target
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_messages(id,conversation_id,sender,client_id,content,reply_to,sequence,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, m.ID, conv, in.Actor, in.ClientID, in.Content, in.ReplyTo, seq, now); err != nil {
		return zero, err
	}
	if s.NotificationQueue != nil {
		if err = s.NotificationQueue(ctx, tx, m); err != nil {
			return zero, err
		}
	}
	if err = s.queueEvent(ctx, tx, m, in.Kind, in.Target); err != nil {
		return zero, err
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return zero, err
	}
	return m, commitMessage(ctx, tx)
}
func (s *DurableStore) queueEvent(ctx context.Context, tx pgx.Tx, m DurableMessage, kind, target string) error {
	channels := []string{s.ChannelNamespace + poolChannelPrefix + target}
	if kind == "dm" {
		rows, err := tx.Query(ctx, `SELECT wallet FROM chat_members WHERE conversation_id=$1 ORDER BY wallet`, m.ConversationID)
		if err != nil {
			return err
		}
		channels = nil
		for rows.Next() {
			var wallet string
			if err = rows.Scan(&wallet); err != nil {
				rows.Close()
				return err
			}
			channels = append(channels, s.ChannelNamespace+dmChannelPrefix+wallet)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ch, err := json.Marshal(channels)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_outbox(event_id,message_id,revision,payload,channels) VALUES($1,$2,$3,$4,$5)`, fmt.Sprintf("%s:%d", m.ID, m.Revision), m.ID, m.Revision, payload, ch)
	return err
}

// Authorized is also the subscription/fetch ACL: inaccessible and absent IDs
// have the same result. No cursor or guessed UUID grants membership.
func (s *DurableStore) Authorized(ctx context.Context, actor, conv string) error {
	if !s.ready() {
		return ErrUnavailable
	}
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND wallet=$2)`, conv, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if s.ReadPolicy != nil {
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer rollback(tx)
		return s.ReadPolicy(ctx, tx, actor, conv)
	}
	return nil
}
func (s *DurableStore) List(ctx context.Context, actor string, limit int) ([]string, error) {
	if !s.ready() {
		return nil, ErrUnavailable
	}
	if !common.IsAddr(actor) || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id FROM chat_conversations c JOIN chat_members m ON m.conversation_id=c.id WHERE m.wallet=$1 AND c.kind='dm' ORDER BY c.id LIMIT $2`, actor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type replayCursor struct {
	Version             int64
	Actor, Conversation string
	After               int64
	Expiry              int64
}

func (s *DurableStore) cursor(actor, conv string, after, version int64) (string, error) {
	if len(s.CursorKey) < 32 {
		return "", ErrUnavailable
	}
	b, err := json.Marshal(replayCursor{Actor: actor, Conversation: conv, After: after, Expiry: s.now().Add(15 * time.Minute).Unix(), Version: version})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *DurableStore) parseCursor(token, actor, conv string) (replayCursor, error) {
	if len(s.CursorKey) < 32 {
		return replayCursor{}, ErrUnavailable
	}
	if len(token) > 2048 {
		return replayCursor{}, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return replayCursor{}, ErrCursor
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return replayCursor{}, ErrCursor
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return replayCursor{}, ErrCursor
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return replayCursor{}, ErrCursor
	}
	var c replayCursor
	if json.Unmarshal(b, &c) != nil || c.Actor != actor || c.Conversation != conv || c.After < 0 || c.Expiry <= s.now().Unix() {
		return replayCursor{}, ErrCursor
	}
	return c, nil
}

type HistoryPage struct {
	ReadThrough int64            `json:"read_through_sequence,string"`
	Reset       bool             `json:"reset_required"`
	Messages    []DurableMessage `json:"messages"`
	Next        string           `json:"next_cursor"`
}

func (s *DurableStore) History(ctx context.Context, actor, conv, token string, limit int) (HistoryPage, error) {
	page := HistoryPage{Messages: []DurableMessage{}}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	if !s.ready() {
		return page, ErrUnavailable
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return page, err
	}
	defer rollback(tx)
	after := int64(0)
	var parsed replayCursor
	if token != "" {
		parsed, err = s.parseCursor(token, actor, conv)
		if err != nil {
			return page, err
		}
		after = parsed.After
	}
	// Query includes membership again: revocation between preliminary ACL and
	// scan cannot release private rows.
	var retained, version int64
	var kind string
	err = tx.QueryRow(ctx, `SELECT retained_from,kind,history_version,m.read_sequence FROM chat_conversations c JOIN chat_members m ON m.conversation_id=c.id WHERE c.id=$1 AND m.wallet=$2`, conv, actor).Scan(&retained, &kind, &version, &page.ReadThrough)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, ErrNotFound
	}
	if err != nil {
		return page, err
	}
	if s.ReadPolicy != nil {
		if err = s.ReadPolicy(ctx, tx, actor, conv); err != nil {
			return page, err
		}
	}
	if token != "" && parsed.Version != version {
		page.Reset = true
		page.Next, _ = s.cursor(actor, conv, retained-1, version)
		return page, ErrSnapshotRequired
	}
	if after < retained-1 {
		page.Next, _ = s.cursor(actor, conv, retained-1, version)
		return page, ErrReplayExpired
	}
	rows, err := tx.Query(ctx, `SELECT `+messageColumns+` FROM chat_messages WHERE conversation_id=$1 AND sequence>$2 AND EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND wallet=$3) ORDER BY sequence LIMIT $4`, conv, after, actor, limit)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scanMessage(rows, kind)
		if e != nil {
			return page, e
		}
		if s.Projection != nil {
			m, e = s.Projection(ctx, m)
			if e != nil {
				return page, e
			}
		}
		page.Messages = append(page.Messages, m)
		after = m.Sequence
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	page.Next, err = s.cursor(actor, conv, after, version)
	return page, err
}

// DrainOutbox is at-least-once delivery. Publisher must use eventID as broker
// dedup key; clients project by message ID/revision. Never claim exactly-once
// transport: a crash after publication before commit intentionally retries.
func (s *DurableStore) DrainOutbox(ctx context.Context, limit int, publish func(context.Context, string, []string, []byte) error) (int, error) {
	if !s.ready() || publish == nil {
		return 0, ErrUnavailable
	}
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	done := 0
	for done < limit {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return done, err
		}
		var id string
		var payload, ch []byte
		err = tx.QueryRow(ctx, `SELECT event_id,payload,channels FROM chat_outbox WHERE delivered_at IS NULL ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &payload, &ch)
		if errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return done, nil
		}
		if err != nil {
			rollback(tx)
			return done, err
		}
		var channels []string
		if err = json.Unmarshal(ch, &channels); err == nil {
			publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = publish(publishCtx, id, channels, payload)
			cancel()
		}
		if err != nil {
			rollback(tx)
			return done, err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_outbox SET delivered_at=now(),attempts=attempts+1 WHERE event_id=$1`, id); err == nil {
			err = tx.Commit(ctx)
		} else {
			rollback(tx)
		}
		if err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// JoinPool persists authorization before subscribing; policy must verify the
// pool exists and current admission/ban rules in the same transaction.
func (s *DurableStore) JoinPool(ctx context.Context, actor, pool string) error {
	if !s.ready() || s.Policy == nil {
		return ErrUnavailable
	}
	if !common.IsAddr(actor) || !common.IsAddr(pool) {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	in := SendInput{Actor: actor, Kind: "pool", Target: pool}
	if err = s.Policy(ctx, tx, in); err != nil {
		return err
	}
	conv := conversation(in)
	if _, err = tx.Exec(ctx, `INSERT INTO chat_conversations(id,kind) VALUES($1,'pool') ON CONFLICT DO NOTHING`, conv); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_members(conversation_id,wallet) VALUES($1,$2) ON CONFLICT DO NOTHING`, conv, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Revise uses optimistic concurrency and leaves a durable tombstone. Original
// sequence is immutable; revision event cannot create a second visible row.
func (s *DurableStore) Revise(ctx context.Context, actor, id string, expected int64, content string, remove bool) (DurableMessage, error) {
	var zero DurableMessage
	if !s.ready() || s.Policy == nil {
		return zero, ErrUnavailable
	}
	if !common.IsAddr(actor) || expected < 1 || len(content) > 16384 || !utf8.ValidString(content) {
		return zero, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer rollback(tx)
	if err = s.lockReserve(ctx, tx); err != nil {
		return zero, err
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return zero, err
	}
	m, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM chat_messages WHERE id=$1 AND sender=$2 AND EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=chat_messages.conversation_id AND wallet=$2) FOR UPDATE`, id, actor), "")
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrNotFound
	}
	if err != nil {
		return zero, err
	}
	if m.Deleted {
		return zero, ErrDeleted
	}
	if m.Revision != expected {
		return zero, ErrConflict
	}
	var kind string
	if err = tx.QueryRow(ctx, `SELECT kind FROM chat_conversations WHERE id=$1`, m.ConversationID).Scan(&kind); err != nil {
		return zero, err
	}
	m.Type = wireKind(kind)
	if kind == "pool" {
		m.PoolAddress = strings.TrimPrefix(m.ConversationID, "pool:")
	}
	target := strings.TrimPrefix(m.ConversationID, "pool:")
	if kind == "dm" {
		if err = tx.QueryRow(ctx, `SELECT wallet FROM chat_members WHERE conversation_id=$1 AND wallet<>$2 LIMIT 1`, m.ConversationID, actor).Scan(&target); err != nil {
			return zero, err
		}
	}
	if err = s.Policy(ctx, tx, SendInput{Actor: actor, Kind: kind, Target: target}); err != nil {
		return zero, err
	}
	if !remove && s.now().Unix()-m.CreatedAt > 900 {
		return zero, ErrInvalid
	}
	content = common.Sanitize(content)
	if !remove && (utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 2000) {
		return zero, ErrInvalid
	}
	m.Revision++
	m.Content = content
	m.Deleted = remove
	if remove {
		m.Content = ""
		m.ReplyTo = ""
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_messages SET content=$2,reply_to=CASE WHEN $5 THEN '' ELSE reply_to END,revision=$3,edited_at=$4::timestamptz,deleted_at=CASE WHEN $5 THEN $4::timestamptz ELSE NULL END WHERE id=$1`, id, m.Content, m.Revision, s.now(), remove); err != nil {
		return zero, err
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_conversations SET history_version=history_version+1 WHERE id=$1`, m.ConversationID); err != nil {
		return zero, err
	}
	if err = s.queueEvent(ctx, tx, m, kind, target); err != nil {
		return zero, err
	}
	return m, tx.Commit(ctx)
}

func (s *DurableStore) MarkRead(ctx context.Context, actor, conv string, through int64) error {
	if !s.ready() {
		return ErrUnavailable
	}
	if through < 0 {
		return ErrInvalid
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE chat_members SET read_sequence=greatest(read_sequence,least($3,c.next_sequence)) FROM chat_conversations c WHERE chat_members.conversation_id=c.id AND c.id=$1 AND wallet=$2`, conv, actor, through)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func wireKind(kind string) string {
	if kind == "pool" {
		return "pool_message"
	}
	return kind
}

// Cancellation must not cancel transaction cleanup. A bounded independent
// context gives pgx a chance to rollback and release its connection safely.
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func commitMessage(ctx context.Context, tx pgx.Tx) error {
	err := tx.Commit(ctx)
	if err == nil || errors.Is(err, pgx.ErrTxCommitRollback) {
		return err
	}
	return errors.Join(ErrCommitUncertain, err)
}

func (s *DurableStore) ModerateTx(ctx context.Context, tx pgx.Tx, id string) error {
	if tx == nil {
		return ErrUnavailable
	}
	m, e := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM chat_messages WHERE id=$1 FOR UPDATE`, id), "")
	if e != nil {
		return e
	}
	if m.Deleted {
		return nil
	}
	var kind string
	if e = tx.QueryRow(ctx, `SELECT kind FROM chat_conversations WHERE id=$1 FOR UPDATE`, m.ConversationID).Scan(&kind); e != nil {
		return e
	}
	m.Type = wireKind(kind)
	m.Content = ""
	m.ReplyTo = ""
	m.Deleted = true
	m.Revision++
	target := strings.TrimPrefix(m.ConversationID, "pool:")
	if kind == "pool" {
		m.PoolAddress = target
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_messages SET content='',reply_to='',deleted_at=clock_timestamp(),revision=$2 WHERE id=$1`, id, m.Revision); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_conversations SET history_version=history_version+1 WHERE id=$1`, m.ConversationID); e != nil {
		return e
	}
	return s.queueEvent(ctx, tx, m, kind, target)
}

func (s *DurableStore) CurrentEvent(ctx context.Context, payload []byte) ([]byte, error) {
	if !s.ready() {
		return nil, ErrUnavailable
	}
	var incoming DurableMessage
	if e := json.Unmarshal(payload, &incoming); e != nil {
		return nil, e
	}
	if incoming.Type == "read_marker" && incoming.ID == "" {
		return payload, nil
	}
	if incoming.ID == "" {
		return nil, ErrInvalid
	}
	current, e := scanMessage(s.Pool.QueryRow(ctx, `SELECT `+messageColumns+` FROM chat_messages WHERE id=$1`, incoming.ID), "")
	if e != nil {
		return nil, e
	}
	if current.ConversationID != incoming.ConversationID || current.Sequence != incoming.Sequence {
		return nil, ErrConflict
	}
	var kind string
	if e = s.Pool.QueryRow(ctx, `SELECT kind FROM chat_conversations WHERE id=$1`, current.ConversationID).Scan(&kind); e != nil {
		return nil, e
	}
	current.Type = wireKind(kind)
	if current.Type != incoming.Type {
		return nil, ErrConflict
	}
	if kind == "pool" {
		current.PoolAddress = strings.TrimPrefix(current.ConversationID, "pool:")
	}
	if s.Projection != nil {
		current, e = s.Projection(ctx, current)
		if e != nil {
			return nil, e
		}
	}
	return json.Marshal(current)
}
