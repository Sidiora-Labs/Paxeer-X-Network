package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"reflect"
	"strings"
	"time"
)

const ConnectedSchemaSQL = `
ALTER TABLE chat_members ADD COLUMN muted boolean NOT NULL DEFAULT false;
ALTER TABLE chat_members ADD COLUMN notification_enabled boolean NOT NULL DEFAULT false;
CREATE TABLE chat_broker_inbox(event_id text PRIMARY KEY,payload_hash text NOT NULL,broker_stream text NOT NULL,broker_sequence bigint NOT NULL CHECK(broker_sequence>0));
CREATE TABLE chat_live_outbox(event_id text NOT NULL REFERENCES chat_broker_inbox(event_id),channel text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),delivered_at timestamptz,PRIMARY KEY(event_id,channel));
CREATE INDEX chat_live_pending ON chat_live_outbox(created_at,event_id) WHERE delivered_at IS NULL;
CREATE TABLE chat_control_outbox(event_id text PRIMARY KEY, wallet text NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), delivered_at timestamptz);
CREATE INDEX chat_control_pending ON chat_control_outbox(created_at,event_id) WHERE delivered_at IS NULL;
CREATE TABLE chat_notification_intents(event_id text NOT NULL, account_id uuid NOT NULL, conversation_id text NOT NULL, message_id text NOT NULL REFERENCES chat_messages(id), created_at timestamptz NOT NULL DEFAULT now(), delivered_at timestamptz, PRIMARY KEY(event_id,account_id));
CREATE INDEX chat_notification_pending ON chat_notification_intents(created_at,event_id) WHERE delivered_at IS NULL;
`

type NotificationTargets interface {
	AccountForWallet(context.Context, pgx.Tx, string) (string, error)
	MutedConversation(context.Context, pgx.Tx, string, string) (bool, error)
}
type NotificationIntentWriter struct {
	Targets           NotificationTargets
	MaximumRecipients int
}

func (w NotificationIntentWriter) Queue(ctx context.Context, tx pgx.Tx, message DurableMessage) error {
	if w.Targets == nil || tx == nil || w.MaximumRecipients < 1 || w.MaximumRecipients > 1000 {
		return ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT m.wallet FROM chat_members m JOIN chat_conversations c ON c.id=m.conversation_id WHERE m.conversation_id=$1 AND m.wallet<>$2 AND (c.kind='dm' OR m.notification_enabled) ORDER BY m.wallet LIMIT $3`, message.ConversationID, message.Sender, w.MaximumRecipients+1)
	if err != nil {
		return err
	}
	var wallets []string
	for rows.Next() {
		var wallet string
		if err = rows.Scan(&wallet); err != nil {
			rows.Close()
			return err
		}
		wallets = append(wallets, wallet)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(wallets) > w.MaximumRecipients {
		return ErrUnavailable
	}
	for _, wallet := range wallets {
		account, err := w.Targets.AccountForWallet(ctx, tx, wallet)
		if err != nil {
			return err
		}
		if _, err = uuid.Parse(account); err != nil {
			return errors.New("notification account mapping unavailable")
		}
		muted, err := w.Targets.MutedConversation(ctx, tx, wallet, message.ConversationID)
		if err != nil {
			return err
		}
		if muted {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chat_notification_intents(event_id,account_id,conversation_id,message_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, fmt.Sprintf("%s:1", message.ID), account, message.ConversationID, message.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *DurableStore) MarkReadConnected(ctx context.Context, actor, conversation string, through int64) error {
	if !s.ready() || through < 0 {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = s.lockReserve(ctx, tx); err != nil {
		return err
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return err
	}
	var member string
	if err = tx.QueryRow(ctx, `SELECT wallet FROM chat_members WHERE conversation_id=$1 AND wallet=$2 FOR UPDATE`, conversation, actor).Scan(&member); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if s.ReadPolicy != nil {
		if err = s.ReadPolicy(ctx, tx, actor, conversation); err != nil {
			return err
		}
	}
	var read int64
	err = tx.QueryRow(ctx, `UPDATE chat_members m SET read_sequence=greatest(m.read_sequence,least($3,coalesce((SELECT max(sequence) FROM chat_messages WHERE conversation_id=$1 AND deleted_at IS NULL),0))) WHERE m.conversation_id=$1 AND m.wallet=$2 RETURNING read_sequence`, conversation, actor, through).Scan(&read)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	event := map[string]string{"type": "read_marker", "conversation_id": conversation, "through_sequence": itoa64(read)}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_control_outbox(event_id,wallet,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, fmt.Sprintf("read:%s:%s:%d", conversation, actor, read), actor, payload)
	if err != nil {
		return err
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *DurableStore) DrainControlOutbox(ctx context.Context, limit int, publish func(context.Context, string, string, []byte) error) (int, error) {
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
		var id, wallet string
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT event_id,wallet,payload FROM chat_control_outbox WHERE delivered_at IS NULL ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &wallet, &payload)
		if errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return done, nil
		}
		if err != nil {
			rollback(tx)
			return done, err
		}
		publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = publish(publishCtx, id, wallet, payload)
		cancel()
		if err != nil {
			rollback(tx)
			return done, err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_control_outbox SET delivered_at=now() WHERE event_id=$1`, id); err == nil {
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

type PostgresNotificationTargets struct{ identitySchema string }

func NewPostgresNotificationTargets(identitySchema string) (PostgresNotificationTargets, error) {
	if identitySchema == "" || len(identitySchema) > 63 {
		return PostgresNotificationTargets{}, ErrInvalid
	}
	for _, c := range identitySchema {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return PostgresNotificationTargets{}, ErrInvalid
		}
	}
	return PostgresNotificationTargets{identitySchema: pgx.Identifier{identitySchema}.Sanitize()}, nil
}
func (p PostgresNotificationTargets) AccountForWallet(ctx context.Context, tx pgx.Tx, wallet string) (string, error) {
	if p.identitySchema == "" || tx == nil {
		return "", ErrUnavailable
	}
	var account string
	err := tx.QueryRow(ctx, `SELECT chat.social_account_for_wallet($1::text)::text`, wallet).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return account, err
}
func (p PostgresNotificationTargets) MutedConversation(ctx context.Context, tx pgx.Tx, wallet, conversation string) (bool, error) {
	if p.identitySchema == "" || tx == nil {
		return false, ErrUnavailable
	}
	var muted bool
	err := tx.QueryRow(ctx, `SELECT muted FROM chat_members WHERE conversation_id=$1 AND wallet=$2 FOR SHARE`, conversation, wallet).Scan(&muted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return muted, err
}

type NotificationIntent struct {
	EventID, AccountID, ConversationID, MessageID string
	CreatedAt                                     time.Time
}

func (s *DurableStore) DrainNotificationIntents(ctx context.Context, limit int, commit func(context.Context, NotificationIntent) error) (int, error) {
	if !s.ready() || commit == nil {
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
		var intent NotificationIntent
		err = tx.QueryRow(ctx, `SELECT event_id,account_id::text,conversation_id,message_id,created_at FROM chat_notification_intents WHERE delivered_at IS NULL ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&intent.EventID, &intent.AccountID, &intent.ConversationID, &intent.MessageID, &intent.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return done, nil
		}
		if err != nil {
			rollback(tx)
			return done, err
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = commit(bounded, intent)
		cancel()
		if err != nil {
			rollback(tx)
			return done, err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_notification_intents SET delivered_at=now() WHERE event_id=$1 AND account_id=$2`, intent.EventID, intent.AccountID); err == nil {
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

type SocialBrokerEnvelope struct {
	Version     int             `json:"version"`
	EventID     string          `json:"event_id"`
	Channels    []string        `json:"channels"`
	PayloadHash string          `json:"payload_hash"`
	Payload     json.RawMessage `json:"payload"`
}
type SocialBroker struct {
	Publisher       delivery.PersistentPublisher
	Subject, Stream string
}

func (b SocialBroker) Publish(ctx context.Context, eventID string, channels []string, payload []byte) error {
	if b.Publisher == nil || b.Subject == "" || b.Stream == "" || eventID == "" || len(channels) < 1 || len(channels) > 20 || len(payload) > 16384 {
		return ErrUnavailable
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		return err
	}
	envelope := SocialBrokerEnvelope{Version: 2, EventID: eventID, Channels: append([]string(nil), channels...), PayloadHash: delivery.Hash(compact.Bytes()), Payload: append([]byte(nil), compact.Bytes()...)}
	wire, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	ack, err := b.Publisher.Publish(ctx, b.Subject, delivery.Hash([]byte(eventID)), wire)
	if err != nil {
		return err
	}
	if ack.Stream != b.Stream || ack.Sequence < 1 {
		return errors.New("social broker persistence acknowledgement mismatch")
	}
	return nil
}
func (s *DurableStore) ConsumeSocialBroker(ctx context.Context, message *natsdriver.Message, subject, stream string) error {
	if !s.ready() || message == nil || message.Subject != subject || message.Stream != stream || message.Sequence == 0 || len(message.Wire) > 65536 {
		return ErrInvalid
	}
	var envelope SocialBrokerEnvelope
	decoder := json.NewDecoder(bytes.NewReader(message.Wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil {
		return ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	if envelope.Version != 2 || len(envelope.EventID) > 200 || envelope.EventID == "" || len(envelope.Channels) < 1 || len(envelope.Channels) > 20 || delivery.Hash([]byte(envelope.EventID)) != message.EventID || delivery.Hash(envelope.Payload) != envelope.PayloadHash {
		return errors.New("social broker event integrity conflict")
	}
	var event struct {
		Type, ID     string
		Conversation string `json:"conversation_id"`
	}
	if json.Unmarshal(envelope.Payload, &event) != nil || event.Conversation == "" || (event.Type != "dm" && event.Type != "pool_message" && event.Type != "read_marker") {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, channel := range envelope.Channels {
		if seen[channel] || len(channel) > 256 || !(strings.HasPrefix(channel, s.ChannelNamespace+poolChannelPrefix) || strings.HasPrefix(channel, s.ChannelNamespace+dmChannelPrefix)) {
			return ErrInvalid
		}
		seen[channel] = true
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = s.lockReserve(ctx, tx); err != nil {
		return err
	}
	var durablePayload, durableChannels []byte
	if event.Type == "read_marker" {
		var wallet string
		if err = tx.QueryRow(ctx, `SELECT payload,wallet FROM chat_control_outbox WHERE event_id=$1`, envelope.EventID).Scan(&durablePayload, &wallet); err != nil {
			return err
		}
		durableChannels, err = json.Marshal([]string{s.ChannelNamespace + dmChannelPrefix + wallet})
		if err != nil {
			return err
		}
	} else {
		if err = tx.QueryRow(ctx, `SELECT payload,channels FROM chat_outbox WHERE event_id=$1`, envelope.EventID).Scan(&durablePayload, &durableChannels); err != nil {
			return err
		}
	}
	var persisted, received any
	var committedChannels []string
	if json.Unmarshal(durablePayload, &persisted) != nil || json.Unmarshal(envelope.Payload, &received) != nil || !reflect.DeepEqual(persisted, received) || json.Unmarshal(durableChannels, &committedChannels) != nil || !reflect.DeepEqual(committedChannels, envelope.Channels) {
		return errors.New("social event differs from committed publication intent")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO chat_broker_inbox(event_id,payload_hash,broker_stream,broker_sequence) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, envelope.EventID, delivery.Hash(message.Wire), message.Stream, int64(message.Sequence))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var hash string
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM chat_broker_inbox WHERE event_id=$1`, envelope.EventID).Scan(&hash); err != nil {
			return err
		}
		if hash != delivery.Hash(message.Wire) {
			return errors.New("social inbox immutable event conflict")
		}
	}
	for _, channel := range envelope.Channels {
		if _, err = tx.Exec(ctx, `INSERT INTO chat_live_outbox(event_id,channel,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, envelope.EventID, channel, []byte(envelope.Payload)); err != nil {
			return err
		}
	}
	if err = s.checkReserve(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return message.Ack(ctx)
}
func (s *DurableStore) DrainLiveOutbox(ctx context.Context, limit int, redis *goredis.Client) (int, error) {
	if !s.ready() || redis == nil {
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
		var id, channel string
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT event_id,channel,payload FROM chat_live_outbox WHERE delivered_at IS NULL ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &channel, &payload)
		if errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return done, nil
		}
		if err != nil {
			rollback(tx)
			return done, err
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = redis.Publish(bounded, channel, payload).Err()
		cancel()
		if err != nil {
			rollback(tx)
			return done, err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_live_outbox SET delivered_at=now() WHERE event_id=$1 AND channel=$2`, id, channel); err == nil {
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
func (h *Hub) RunConnected(ctx context.Context, consumer *natsdriver.Consumer, broker SocialBroker) error {
	if h.protocolVersion != 2 || h.durable == nil || consumer == nil || broker.Publisher == nil || broker.Subject == "" || broker.Stream == "" {
		return ErrUnavailable
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- h.Run(runCtx) }()
	go func() {
		results <- consumer.Run(runCtx, func(ctx context.Context, message *natsdriver.Message) error {
			return h.durable.ConsumeSocialBroker(ctx, message, broker.Subject, broker.Stream)
		})
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-results:
			return err
		case <-ticker.C:
			if _, err := h.durable.DrainOutbox(runCtx, 100, broker.Publish); err != nil {
				h.logErr("durable social publisher", err)
			}
			if _, err := h.durable.DrainControlOutbox(runCtx, 100, func(ctx context.Context, id, wallet string, payload []byte) error {
				return broker.Publish(ctx, id, []string{h.namespace + dmChannelPrefix + wallet}, payload)
			}); err != nil {
				h.logErr("durable social read publisher", err)
			}
			if _, err := h.durable.DrainLiveOutbox(runCtx, 100, h.pub); err != nil {
				h.logErr("durable social live delivery", err)
			}
		}
	}
}

func (s *DurableStore) lockReserve(ctx context.Context, tx pgx.Tx) error {
	if s.PendingOutboxLimit == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('social-outbox-reserve',36))`)
	return err
}
func (s *DurableStore) checkReserve(ctx context.Context, tx pgx.Tx) error {
	if s.PendingOutboxLimit == 0 {
		return nil
	}
	if s.PendingOutboxLimit < 1 || s.MaxOutboxAge <= 0 {
		return ErrUnavailable
	}
	var count int64
	var oldest *time.Time
	err := tx.QueryRow(ctx, `SELECT count(*),min(created_at) FROM (SELECT created_at FROM chat_outbox WHERE delivered_at IS NULL UNION ALL SELECT created_at FROM chat_control_outbox WHERE delivered_at IS NULL UNION ALL SELECT created_at FROM chat_notification_intents WHERE delivered_at IS NULL UNION ALL SELECT created_at FROM chat_live_outbox WHERE delivered_at IS NULL) pending`).Scan(&count, &oldest)
	if err != nil {
		return err
	}
	if count >= int64(s.PendingOutboxLimit) || oldest != nil && s.now().Sub(*oldest) >= s.MaxOutboxAge {
		return ErrUnavailable
	}
	return nil
}
