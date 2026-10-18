package voice

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type LiveKit struct {
	Endpoint, APIKey, APISecret string
	Client                      *http.Client
	Clock                       func() time.Time
	RecordingOutput             S3RecordingOutput
}
type S3RecordingOutput struct {
	Endpoint       string `json:"endpoint,omitempty"`
	ForcePathStyle bool   `json:"force_path_style"`
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	AccessKey      string `json:"access_key"`
	Secret         string `json:"secret"`
	SessionToken   string `json:"session_token,omitempty"`
}
type providerError struct {
	Code string `json:"code"`
}

func (e providerError) Error() string { return "voice provider rejected operation: " + e.Code }
func NewLiveKit(endpoint, key, secret string, output S3RecordingOutput) (*LiveKit, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".livekit.cloud") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || key == "" || len(secret) < 32 {
		return nil, ErrUnavailable
	}
	return &LiveKit{Endpoint: strings.TrimRight(endpoint, "/"), APIKey: key, APISecret: secret, Client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, RecordingOutput: output}, nil
}
func (p *LiveKit) now() time.Time {
	if p.Clock != nil {
		return p.Clock().UTC()
	}
	return time.Now().UTC()
}
func (p *LiveKit) token(identity string, grants map[string]any, ttl time.Duration) (string, error) {
	if p.APIKey == "" || len(p.APISecret) < 32 || ttl <= 0 || ttl > 5*time.Minute {
		return "", ErrUnavailable
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := p.now()
	claims := map[string]any{"iss": p.APIKey, "nbf": now.Unix(), "exp": now.Add(ttl).Unix(), "iat": now.Unix(), "video": grants}
	if identity != "" {
		claims["sub"] = identity
	}
	b, e := json.Marshal(claims)
	if e != nil {
		return "", e
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(p.APISecret))
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (p *LiveKit) call(ctx context.Context, service, method, room string, input, output any) error {
	if p == nil || p.Client == nil || p.Endpoint == "" {
		return ErrUnavailable
	}
	grants := map[string]any{"roomCreate": true, "roomList": true, "roomAdmin": true, "roomRecord": true}
	if room != "" {
		grants["room"] = room
	}
	token, e := p.token("", grants, 30*time.Second)
	if e != nil {
		return e
	}
	body, e := json.Marshal(input)
	if e != nil {
		return e
	}
	call, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(call, http.MethodPost, p.Endpoint+"/twirp/livekit."+service+"/"+method, bytes.NewReader(body))
	if e != nil {
		return ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := p.Client.Do(req)
	if e != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil || len(b) >= 1<<20 {
		return ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		var pe providerError
		if json.Unmarshal(b, &pe) != nil || pe.Code == "" {
			return ErrUnavailable
		}
		return pe
	}
	if output == nil {
		return nil
	}
	var value any
	if json.Unmarshal(b, &value) != nil {
		return ErrUnavailable
	}
	normalizeProviderJSON(value)
	normalized, e := json.Marshal(value)
	if e != nil || json.Unmarshal(normalized, output) != nil {
		return ErrUnavailable
	}
	return nil
}
func (p *LiveKit) Find(ctx context.Context, key string) (string, bool, error) {
	name := "kindle-" + key
	var response struct {
		Rooms []struct {
			Name string `json:"name"`
		} `json:"rooms"`
	}
	if e := p.call(ctx, "RoomService", "ListRooms", "", map[string]any{"names": []string{name}}, &response); e != nil {
		return "", false, e
	}
	for _, r := range response.Rooms {
		if r.Name == name {
			return name, true, nil
		}
	}
	return "", false, nil
}
func (p *LiveKit) Start(ctx context.Context, key string) (string, error) {
	if key == "" || len(key) > 160 {
		return "", ErrUnavailable
	}
	if name, ok, e := p.Find(ctx, key); e != nil {
		return "", e
	} else if ok {
		return name, nil
	}
	name := "kindle-" + key
	var result struct {
		Name string `json:"name"`
	}
	if e := p.call(ctx, "RoomService", "CreateRoom", name, map[string]any{"name": name, "empty_timeout": 300, "departure_timeout": 20, "max_participants": 50}, &result); e != nil {
		return "", e
	}
	if result.Name != name {
		return "", ErrUnavailable
	}
	return name, nil
}
func (p *LiveKit) End(ctx context.Context, room string) error {
	var participants struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	listErr := p.call(ctx, "RoomService", "ListParticipants", room, map[string]string{"room": room}, &participants)
	var missing providerError
	if listErr != nil && !(errors.As(listErr, &missing) && missing.Code == "not_found") {
		return listErr
	}
	if len(participants.Participants) > 50 {
		return ErrUnavailable
	}
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	errs := make(chan error, len(participants.Participants))
	for _, participant := range participants.Participants {
		identity := participant.Identity
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			defer func() { <-sem }()
			if e := p.Revoke(ctx, room, identity); e != nil {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	e := p.call(ctx, "RoomService", "DeleteRoom", room, map[string]string{"room": room}, nil)
	var pe providerError
	if e != nil && !(errors.As(e, &pe) && pe.Code == "not_found") {
		return e
	}
	var result struct {
		Rooms []struct {
			Name string `json:"name"`
		} `json:"rooms"`
	}
	if e = p.call(ctx, "RoomService", "ListRooms", "", map[string]any{"names": []string{room}}, &result); e != nil {
		return e
	}
	if len(result.Rooms) != 0 {
		return ErrUnavailable
	}
	return nil
}
func (p *LiveKit) Revoke(ctx context.Context, room, wallet string) error {
	e := p.call(ctx, "RoomService", "RemoveParticipant", room, map[string]string{"room": room, "identity": wallet, "revoke_token_ts": strconv.FormatInt(p.now().Unix()+1, 10)}, nil)
	var pe providerError
	if errors.As(e, &pe) && pe.Code == "not_found" {
		return nil
	}
	return e
}
func (p *LiveKit) Grant(ctx context.Context, room, wallet string, publish bool) error {
	var result struct {
		Identity string `json:"identity"`
	}
	permission := map[string]any{"can_publish": publish, "can_subscribe": true, "can_publish_data": false, "can_publish_sources": []string{"MICROPHONE"}}
	if e := p.call(ctx, "RoomService", "UpdateParticipant", room, map[string]any{"room": room, "identity": wallet, "permission": permission}, &result); e != nil {
		return e
	}
	if result.Identity != wallet {
		return ErrUnavailable
	}
	return nil
}
func (p *LiveKit) Ticket(_ context.Context, room, wallet string, publish bool, ttl time.Duration) (string, error) {
	if room == "" || wallet == "" || ttl > time.Minute {
		return "", ErrUnavailable
	}
	return p.token(wallet, map[string]any{"roomJoin": true, "room": room, "canSubscribe": true, "canPublish": publish, "canPublishData": false, "canPublishSources": []string{"microphone"}, "canUpdateOwnMetadata": false}, ttl)
}
func (p *LiveKit) FindRecording(ctx context.Context, room, key string) (string, bool, error) {
	var listed struct {
		Items []struct {
			ID      string `json:"egress_id"`
			Status  string `json:"status"`
			Request struct {
				Files []struct {
					Path string `json:"filepath"`
				} `json:"file_outputs"`
			} `json:"room_composite"`
		} `json:"items"`
	}
	path := "restricted/voice/" + key + ".ogg"
	if e := p.call(ctx, "Egress", "ListEgress", room, map[string]any{"room_name": room}, &listed); e != nil {
		return "", false, e
	}
	for _, item := range listed.Items {
		for _, f := range item.Request.Files {
			if f.Path == path {
				if item.Status != "EGRESS_STARTING" && item.Status != "EGRESS_ACTIVE" {
					return "", false, ErrUnavailable
				}
				return item.ID, true, nil
			}
		}
	}
	return "", false, nil
}
func (p *LiveKit) Record(ctx context.Context, room, key string) (string, error) {
	o := p.RecordingOutput
	if o.Bucket == "" || o.Region == "" || o.AccessKey == "" || o.Secret == "" {
		return "", ErrUnavailable
	}
	if id, exists, e := p.FindRecording(ctx, room, key); e != nil {
		return "", e
	} else if exists {
		return id, nil
	}
	var result struct {
		ID string `json:"egress_id"`
	}
	path := "restricted/voice/" + key + ".ogg"
	if e := p.call(ctx, "Egress", "StartRoomCompositeEgress", room, map[string]any{"room_name": room, "audio_only": true, "file_outputs": []any{map[string]any{"file_type": "OGG", "filepath": path, "disable_manifest": true, "s3": o}}}, &result); e != nil {
		return "", e
	}
	if result.ID == "" {
		return "", ErrUnavailable
	}
	return result.ID, nil
}
func (p *LiveKit) StopRecording(ctx context.Context, room, id string) error {
	var listed struct {
		Items []struct {
			ID     string `json:"egress_id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if e := p.call(ctx, "Egress", "ListEgress", room, map[string]string{"egress_id": id}, &listed); e != nil {
		return e
	}
	for _, item := range listed.Items {
		if item.ID == id && (item.Status == "EGRESS_COMPLETE" || item.Status == "EGRESS_ABORTED" || item.Status == "EGRESS_FAILED") {
			return nil
		}
	}
	var result struct {
		Status string `json:"status"`
	}
	if e := p.call(ctx, "Egress", "StopEgress", room, map[string]string{"egress_id": id}, &result); e != nil {
		return e
	}
	if result.Status != "EGRESS_COMPLETE" && result.Status != "EGRESS_ABORTED" && result.Status != "EGRESS_FAILED" {
		return ErrUnavailable
	}
	return nil
}

func normalizeProviderJSON(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			normalizeProviderJSON(item)
			var out strings.Builder
			for _, ch := range key {
				if ch >= 'A' && ch <= 'Z' {
					out.WriteByte('_')
					out.WriteRune(ch + ('a' - 'A'))
				} else {
					out.WriteRune(ch)
				}
			}
			normalized := out.String()
			if normalized != key {
				v[normalized] = item
				delete(v, key)
			}
		}
	case []any:
		for _, item := range v {
			normalizeProviderJSON(item)
		}
	}
}

func NewProductionControl(db *pgxpool.Pool, policy Policy, pools Pools, provider *LiveKit, gates Gates, policyVersion string, recording RecordingPolicy, recordingStorage *storage.Client, recordingBucket string) (*Control, error) {
	if db == nil || policy == nil || pools == nil || provider == nil || provider.Client == nil || !gates.Ready() || !safety.Bounded(policyVersion, 160) {
		return nil, ErrUnavailable
	}
	if _, e := NewLiveKit(provider.Endpoint, provider.APIKey, provider.APISecret, provider.RecordingOutput); e != nil {
		return nil, e
	}
	c := &Control{DB: db, Policy: policy, Pools: pools, Provider: provider, Gates: gates, SafetyPolicyVersion: policyVersion, RecordingPolicy: recording, RecordingStorage: recordingStorage}
	if recording.Enabled && (!c.recordingReady() || recordingStorage == nil || recordingBucket == "" || recordingBucket != provider.RecordingOutput.Bucket) {
		return nil, ErrUnavailable
	}
	return c, nil
}
