package voice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protowire"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNativeLiveKitListenerGrantCannotPublish(t *testing.T) {
	secret := strings.Repeat("s", 32)
	p := &LiveKit{APIKey: "owned-native-key", APISecret: secret, Clock: func() time.Time { return time.Unix(1700000000, 0) }}
	token, e := p.Ticket(context.Background(), "owned-room", host, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("JWT segments")
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		t.Fatal(e)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		t.Fatal("real signature invalid")
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		t.Fatal(e)
	}
	var claims struct {
		Subject   string `json:"sub"`
		Expires   int64  `json:"exp"`
		NotBefore int64  `json:"nbf"`
		Video     struct {
			Room    string `json:"room"`
			Publish bool   `json:"canPublish"`
			Data    bool   `json:"canPublishData"`
			Join    bool   `json:"roomJoin"`
		} `json:"video"`
	}
	if e = json.Unmarshal(raw, &claims); e != nil {
		t.Fatal(e)
	}
	if claims.Subject != host || claims.Video.Room != "owned-room" || claims.Video.Publish || claims.Video.Data || !claims.Video.Join || claims.Expires-claims.NotBefore != 60 {
		t.Fatal("listener privilege or TTL violation", claims)
	}
	if _, e = p.Ticket(context.Background(), "owned-room", host, true, 6*time.Minute); !errors.Is(e, ErrUnavailable) {
		t.Fatal("oversized TTL accepted", e)
	}
}
func TestPostgresDeclinedConsentStopsBeforeProviderContact(t *testing.T) {
	c, ctx := fixture(t)
	ddl, e := os.ReadFile("../../../../spec/production-system/handoffs/L38/phase2/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	c.RecordingPolicy = RecordingPolicy{Enabled: true, Version: "explicit-v2", Purpose: "Community session archive", Retention: 24 * time.Hour, RestrictedStorageQualified: true}
	room := Room{ID: uuid.NewString()}
	c.Policy = &safety.Store{DB: c.DB}
	if _, e = c.DB.Exec(ctx, `INSERT INTO voice_rooms(id,pool_id,host_wallet,start_key,state,provider_session)VALUES($1,'known-pool',$2,$3,'live','uncontacted-consent-fixture')`, room.ID, host, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(ctx, `INSERT INTO voice_participants(room_id,wallet,role)VALUES($1,$2,'host')`, room.ID, host); e != nil {
		t.Fatal(e)
	}
	provider := &LiveKit{}
	c.Provider = provider
	c.Gates = Gates{true, true, true, true}
	rec, e := c.RequestRecording(ctx, room.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Consent(ctx, room.ID, rec.ID, rec.Revision, false); e != nil {
		t.Fatal(e)
	}
	if _, e = c.StartConsentedRecording(ctx, room.ID, rec.Revision); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("declined consent reached provider", e)
	}
	persisted, e := c.Recording(ctx, room.ID)
	if e != nil || persisted.State != "consent_pending" || persisted.ProviderID != "" {
		t.Fatal("recording falsely started", persisted, e)
	}
	if _, e = c.Ticket(ctx, room.ID); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("declined participant received grant", e)
	}
}
func TestRealLiveKitProviderOwnedSessionLifecycle(t *testing.T) {
	endpoint, key, secret := os.Getenv("L38_LIVEKIT_URL"), os.Getenv("L38_LIVEKIT_API_KEY"), os.Getenv("L38_LIVEKIT_API_SECRET")
	p, e := NewLiveKit(endpoint, key, secret, S3RecordingOutput{})
	if e != nil {
		t.Fatal("supported real LiveKit Cloud test project required", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	startKey := "phase2-" + strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "-")
	room, e := p.Start(ctx, startKey)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := p.End(context.Background(), room); e != nil {
			t.Error(e)
		}
	}()
	again, e := p.Start(ctx, startKey)
	if e != nil || again != room {
		t.Fatal("idempotent provider session not adopted", again, e)
	}
	if _, found, e := p.Find(ctx, startKey); e != nil || !found {
		t.Fatal("provider session absent", e)
	}
}

func phase3Provider(t *testing.T) *LiveKit {
	t.Helper()
	p, e := NewLiveKit(os.Getenv("L38_LIVEKIT_URL"), os.Getenv("L38_LIVEKIT_API_KEY"), os.Getenv("L38_LIVEKIT_API_SECRET"), S3RecordingOutput{})
	if e != nil {
		t.Fatal("real supported LiveKit Cloud project required", e)
	}
	return p
}
func phase3Schema(t *testing.T, c *Control, ctx context.Context) {
	t.Helper()
	ddl, e := os.ReadFile("../../../../spec/production-system/handoffs/L38/phase2/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	c.Policy = &safety.Store{DB: c.DB}
	c.Gates = Gates{true, true, true, true}
}
func phase3Room(t *testing.T, c *Control, ctx context.Context) Room {
	t.Helper()
	r := Room{ID: uuid.NewString(), StartKey: uuid.NewString()}
	if _, e := c.DB.Exec(ctx, `INSERT INTO voice_rooms(id,pool_id,host_wallet,start_key)VALUES($1,'known-pool',$2,$3)`, r.ID, host, r.StartKey); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRealLiveKitCreateCrashAdoptsOneSessionAndEndCrashReconciles(t *testing.T) {
	c, ctx := fixture(t)
	phase3Schema(t, c, ctx)
	p := phase3Provider(t)
	c.Provider = p
	r := phase3Room(t, c, ctx)
	if _, e := c.DB.Exec(ctx, `CREATE FUNCTION voice_phase3_interrupt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state IN ('live','ended') THEN RAISE EXCEPTION 'owned database-response interruption'; END IF; RETURN NEW; END $$; CREATE TRIGGER voice_phase3_interrupt BEFORE UPDATE ON voice_rooms FOR EACH ROW EXECUTE FUNCTION voice_phase3_interrupt()`); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Start(ctx, r.ID, 1); e == nil {
		t.Fatal("injected database response interruption accepted")
	}
	defer func() {
		if e := p.End(context.Background(), "kindle-"+r.StartKey); e != nil {
			t.Error(e)
		}
	}()
	session, found, e := p.Find(ctx, r.StartKey)
	if e != nil || !found {
		t.Fatal("provider create did not survive database rollback", e)
	}
	persisted, e := c.Read(ctx, r.ID)
	if e != nil || persisted.State != "draft" || persisted.ProviderSession != "" {
		t.Fatal(persisted, e)
	}
	if _, e = c.DB.Exec(ctx, `DROP TRIGGER voice_phase3_interrupt ON voice_rooms`); e != nil {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(ctx, `UPDATE voice_rooms SET updated_at=now()-interval '6 minutes' WHERE id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	restarted := &Control{DB: c.DB, Provider: p, Policy: c.Policy, Gates: c.Gates}
	result, e := restarted.Reconcile(ctx)
	if e != nil || result.Adopted != 1 || result.Orphans != 1 {
		t.Fatal("orphan alarm/adoption missing", result, e)
	}
	persisted, e = restarted.Read(ctx, r.ID)
	if e != nil || persisted.State != "live" || persisted.ProviderSession != session {
		t.Fatal(persisted, e)
	}
	again, e := restarted.Reconcile(ctx)
	if e != nil || again.Adopted != 0 || again.Terminated != 0 {
		t.Fatal("reconciliation is not idempotent", again, e)
	}
	if _, e = c.DB.Exec(ctx, `CREATE TRIGGER voice_phase3_interrupt BEFORE UPDATE ON voice_rooms FOR EACH ROW WHEN (NEW.state='ended') EXECUTE FUNCTION voice_phase3_interrupt()`); e != nil {
		t.Fatal(e)
	}
	if _, e = restarted.End(ctx, r.ID, persisted.Revision); e == nil {
		t.Fatal("end response interruption accepted")
	}
	ending, e := restarted.Read(ctx, r.ID)
	if e != nil || ending.State != "ending" {
		t.Fatal("cleanup falsely ended", ending, e)
	}
	if _, e = c.DB.Exec(ctx, `DROP TRIGGER voice_phase3_interrupt ON voice_rooms`); e != nil {
		t.Fatal(e)
	}
	finished, e := restarted.Reconcile(ctx)
	if e != nil || finished.Terminated != 1 {
		t.Fatal(finished, e)
	}
	persisted, e = restarted.Read(ctx, r.ID)
	if e != nil || persisted.State != "ended" {
		t.Fatal(persisted, e)
	}
	if _, e = restarted.Ticket(ctx, r.ID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("ended room minted token", e)
	}
	t.Logf(`{"scenario":"provider-create-end-crash","reconciliation":{"adopted":%d,"terminated":%d,"orphans":%d}}`, result.Adopted, finished.Terminated, result.Orphans)
}
func TestRealLiveKitSpeakerEjectionClosesConnectionAndRejectsOldTicket(t *testing.T) {
	c, ctx := fixture(t)
	phase3Schema(t, c, ctx)
	p := phase3Provider(t)
	c.Provider = p
	r := phase3Room(t, c, ctx)
	live, e := c.Start(ctx, r.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := p.End(context.Background(), live.ProviderSession); e != nil {
			t.Error(e)
		}
	}()
	if e = c.Join(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	ticket, e := c.Ticket(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	address := "wss://" + strings.TrimPrefix(p.Endpoint, "https://") + "/rtc?access_token=" + url.QueryEscape(ticket) + "&auto_subscribe=1&protocol=15&sdk=go&version=1.0.0"
	dialer := websocket.Dialer{HandshakeTimeout: 4 * time.Second}
	conn, response, e := dialer.DialContext(ctx, address, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if e != nil {
		t.Fatal("real provider signaling join failed", e)
	}
	defer conn.Close()
	if e = conn.SetReadDeadline(time.Now().Add(4 * time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, payload, e := conn.ReadMessage(); e != nil || len(payload) == 0 {
		t.Fatal("provider join response absent", e)
	}
	var participant struct {
		Identity   string `json:"identity"`
		Permission struct {
			Publish bool `json:"can_publish"`
		} `json:"permission"`
	}
	readPermission := func() {
		t.Helper()
		if e := p.call(ctx, "RoomService", "GetParticipant", live.ProviderSession, map[string]string{"room": live.ProviderSession, "identity": host}, &participant); e != nil {
			t.Fatal(e)
		}
	}
	readPermission()
	if participant.Identity != host || participant.Permission.Publish {
		t.Fatal("listener transmitted with browser-only role", participant)
	}
	add := protowire.AppendTag(nil, 1, protowire.BytesType)
	add = protowire.AppendString(add, "adversarial-listener-microphone")
	add = protowire.AppendTag(add, 2, protowire.BytesType)
	add = protowire.AppendString(add, "microphone")
	add = protowire.AppendTag(add, 8, protowire.VarintType)
	add = protowire.AppendVarint(add, 2)
	signal := protowire.AppendTag(nil, 4, protowire.BytesType)
	signal = protowire.AppendBytes(signal, add)
	if e = conn.WriteMessage(websocket.BinaryMessage, signal); e != nil {
		t.Fatal(e)
	}
	denied := false
	for !denied {
		_, wire, err := conn.ReadMessage()
		if err != nil {
			t.Fatal("listener publish lacked explicit provider denial", err)
		}
		for len(wire) > 0 {
			field, kind, n := protowire.ConsumeTag(wire)
			if n < 0 {
				t.Fatal("invalid provider signal")
			}
			wire = wire[n:]
			if kind == protowire.BytesType {
				value, n := protowire.ConsumeBytes(wire)
				if n < 0 {
					t.Fatal("invalid provider signal body")
				}
				wire = wire[n:]
				if field == 6 {
					t.Fatal("provider accepted unauthorized listener track")
				}
				if field == 22 {
					for len(value) > 0 {
						key, typ, n := protowire.ConsumeTag(value)
						if n < 0 {
							t.Fatal("invalid request response")
						}
						value = value[n:]
						if key == 2 && typ == protowire.VarintType {
							reason, n := protowire.ConsumeVarint(value)
							if n < 0 {
								t.Fatal("invalid denial")
							}
							denied = reason == 2
							value = value[n:]
						} else {
							n := protowire.ConsumeFieldValue(key, typ, value)
							if n < 0 {
								t.Fatal("invalid request response field")
							}
							value = value[n:]
						}
					}
				}
			} else {
				n := protowire.ConsumeFieldValue(field, kind, wire)
				if n < 0 {
					t.Fatal("invalid provider field")
				}
				wire = wire[n:]
			}
		}
	}
	if e = c.ApproveSpeaker(ctx, r.ID, host, 1); e != nil {
		t.Fatal(e)
	}
	readPermission()
	if !participant.Permission.Publish {
		t.Fatal("provider did not approve real speaker")
	}
	speakerTicket, e := c.Ticket(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if e = c.Mute(ctx, r.ID, host, 3, true); e != nil {
		t.Fatal(e)
	}
	if e = conn.SetReadDeadline(start.Add(5 * time.Second)); e != nil {
		t.Fatal(e)
	}
	for {
		_, _, e = conn.ReadMessage()
		if e != nil {
			var timeout net.Error
			if errors.As(e, &timeout) && timeout.Timeout() {
				t.Fatal("provider never closed revoked connection", e)
			}
			break
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatal("speaker connection survived revocation", elapsed)
	}
	staleAddress := strings.Replace(address, url.QueryEscape(ticket), url.QueryEscape(speakerTicket), 1)
	reused, response, e := dialer.DialContext(ctx, staleAddress, nil)
	if reused != nil {
		reused.Close()
		t.Fatal("old speaker ticket rejoined after provider revocation")
	}
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if e == nil || response == nil || (response.StatusCode != 401 && response.StatusCode != 403) {
		t.Fatal("provider did not explicitly reject stale token", e)
	}
	if e = c.Join(ctx, r.ID); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("ejected participant joined again", e)
	}
	if _, e = c.Ticket(ctx, r.ID); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("ejected participant received replacement token", e)
	}
	t.Logf(`{"scenario":"speaker-ejection-token-reuse","revocation_latency_ms":%d}`, time.Since(start).Milliseconds())
}

func TestPostgresRecordingAcknowledgementAfterEndPersistsCleanupIntent(t *testing.T) {
	c, ctx := fixture(t)
	phase3Schema(t, c, ctx)
	room := phase3Room(t, c, ctx)
	recording := uuid.NewString()
	if _, e := c.DB.Exec(ctx, `UPDATE voice_rooms SET state='ended' WHERE id=$1`, room.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := c.DB.Exec(ctx, `INSERT INTO voice_recordings(id,room_id,state,purpose,policy_version,delete_at)VALUES($1,$2,'starting','explicit test purpose','v3',now()+interval '1 day')`, recording, room.ID); e != nil {
		t.Fatal(e)
	}
	p, e := safety.Actor(ctx, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = c.recordingTransition(ctx, recording, room.ID, 1, "starting", "recording", "recording-response-interruption", p); !errors.Is(e, safety.ErrConflict) {
		t.Fatal("ended room acknowledged recording start", e)
	}
	var state, provider string
	var revision int64
	if e = c.DB.QueryRow(ctx, `SELECT state,provider_id,revision FROM voice_recordings WHERE id=$1`, recording).Scan(&state, &provider, &revision); e != nil || state != "stopping" || provider != "recording-response-interruption" || revision != 2 {
		t.Fatal("provider cleanup intent lost", state, provider, revision, e)
	}
	var events int
	if e = c.DB.QueryRow(ctx, `SELECT count(*) FROM voice_recording_events WHERE recording_id=$1 AND kind='recording_stopping'`, recording).Scan(&events); e != nil || events != 1 {
		t.Fatal(events, e)
	}
}
