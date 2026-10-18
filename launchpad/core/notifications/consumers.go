package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Endpoint struct {
	Token      string
	URL        string
	PublicKey  string
	Auth       string
	Permission string
}
type TargetAuthority interface {
	PreferenceConsumer
	ResolveEndpoint(context.Context, PushTarget) (Endpoint, error)
	DisableTarget(context.Context, PushTarget, string) error
	DeleteTargets(context.Context, string) error
}

type TargetLister interface {
	Targets(context.Context, string) ([]PushTarget, error)
}
type TriggerConsumer struct {
	Store     Store
	Authority TargetAuthority
}

func (c TriggerConsumer) Publish(ctx context.Context, kind, id string, raw json.RawMessage) error {
	if kind != "threshold-crossing-v2" {
		return ErrInvalid
	}
	var trigger discovery.AlertTrigger
	if e := json.Unmarshal(raw, &trigger); e != nil {
		return e
	}
	if trigger.ID != id || trigger.Rule.Validate() != nil || trigger.CreatedAt.IsZero() {
		return ErrInvalid
	}
	lister, ok := c.Authority.(TargetLister)
	if !ok {
		return ErrUnavailable
	}
	targets, e := lister.Targets(ctx, trigger.Rule.OwnerAccountID)
	if e != nil {
		return e
	}
	deliveries := []DeliveryAttempt{}
	push := false
	for _, channel := range trigger.Rule.Channels {
		if channel == "push" {
			push = true
		} else if channel != "inapp" {
			return ErrUnavailable
		}
	}
	if push {
		for _, target := range targets {
			if target.AccountID != trigger.Rule.OwnerAccountID {
				return ErrForbidden
			}
			if target.Status == "active" {
				deliveries = append(deliveries, DeliveryAttempt{Channel: "push", TargetID: target.DeviceInstallationID, TargetRevision: target.Revision, ConsentRevision: target.ConsentRevision})
			}
		}
	}
	item := InboxItem{Identity: Identity{AccountID: trigger.Rule.OwnerAccountID, TriggerEventID: id, TemplateVersion: "market-crossing-v2"}, Category: "watchlist", Summary: "A watched pool crossed your threshold", ResourceID: trigger.Rule.PoolAddress, CreatedAt: trigger.CreatedAt, ExpiresAt: trigger.CreatedAt.Add(15 * time.Minute)}
	_, _, e = c.Store.CommitTrigger(ctx, item, deliveries)
	return e
}

type accountRPC struct {
	base        string
	client      *http.Client
	credentials *Credentials
	tokenRef    string
}

func newAccountRPC(base string, client *http.Client, credentials *Credentials, tokenRef string) (accountRPC, error) {
	var rpc accountRPC
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || client == nil || credentials == nil {
		return rpc, ErrInvalid
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || (len(transport.TLSClientConfig.Certificates) == 0 && transport.TLSClientConfig.GetClientCertificate == nil) || client.Timeout <= 0 || client.Timeout > 10*time.Second {
		return rpc, ErrInvalid
	}
	if _, e = credentials.Get(tokenRef); e != nil {
		return rpc, e
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return accountRPC{strings.TrimRight(base, "/"), &copyClient, credentials, tokenRef}, nil
}
func (r accountRPC) call(ctx context.Context, method, path string, input, output any) error {
	token, e := r.credentials.Get(r.tokenRef)
	if e != nil {
		return e
	}
	var raw []byte
	if input != nil {
		raw, e = json.Marshal(input)
		if e != nil {
			return e
		}
	}
	request, e := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, e := r.client.Do(request)
	if e != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == 404 || response.StatusCode == 403 || response.StatusCode == 401 {
		return ErrForbidden
	}
	if response.StatusCode != 200 && response.StatusCode != 204 {
		return ErrUnavailable
	}
	if output == nil {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 131073))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(output); e != nil {
		return ErrUnavailable
	}
	var extra any
	if e = decoder.Decode(&extra); e != io.EOF {
		return ErrUnavailable
	}
	return nil
}

type AccountClient struct{ rpc accountRPC }

func NewAccountClient(base string, client *http.Client, credentials *Credentials, tokenRef string) (*AccountClient, error) {
	rpc, e := newAccountRPC(base, client, credentials, tokenRef)
	if e != nil {
		return nil, e
	}
	return &AccountClient{rpc: rpc}, nil
}
func accountPath(account string) string { return "/internal/v2/accounts/" + url.PathEscape(account) }
func (c *AccountClient) CurrentDeliveryPreference(ctx context.Context, account string) (DeliveryPreference, error) {
	var p DeliveryPreference
	if account == "" {
		return p, ErrInvalid
	}
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/delivery-preference", nil, &p)
	if e != nil {
		return p, e
	}
	if p.AccountID != account || p.Revision == 0 || p.CategoryChannels == nil {
		return p, ErrUnavailable
	}
	if _, e = time.LoadLocation(p.Timezone); e != nil {
		return p, ErrUnavailable
	}
	for category, channels := range p.CategoryChannels {
		switch category {
		case "trade", "watchlist", "creator", "chat", "security", "marketing":
		default:
			return p, ErrUnavailable
		}
		for channel := range channels {
			switch channel {
			case "push", "inapp", "email":
			default:
				return p, ErrUnavailable
			}
		}
	}
	return p, nil
}
func (c *AccountClient) Targets(ctx context.Context, account string) ([]PushTarget, error) {
	targets := []PushTarget{}
	if account == "" {
		return nil, ErrInvalid
	}
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/targets", nil, &targets)
	if e != nil {
		return nil, e
	}
	if len(targets) > 32 {
		return nil, ErrUnavailable
	}
	seen := map[string]bool{}
	for _, t := range targets {
		if targetValid(t, account) != nil || seen[t.DeviceInstallationID] {
			return nil, ErrUnavailable
		}
		seen[t.DeviceInstallationID] = true
	}
	return targets, nil
}
func targetValid(t PushTarget, account string) error {
	if t.AccountID != account || t.DeviceInstallationID == "" || t.Revision == 0 || t.ConsentRevision == 0 || t.EncryptedEndpointRef == "" {
		return ErrUnavailable
	}
	switch t.Provider {
	case "apns", "fcm", "webpush":
	default:
		return ErrUnavailable
	}
	if t.Status != "active" && t.Status != "revoked" && t.Status != "disabled" {
		return ErrUnavailable
	}
	return nil
}
func (c *AccountClient) CurrentTarget(ctx context.Context, account, device string) (PushTarget, error) {
	var t PushTarget
	if account == "" || device == "" {
		return t, ErrInvalid
	}
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/targets/"+url.PathEscape(device), nil, &t)
	if e != nil {
		return t, e
	}
	if t.DeviceInstallationID != device {
		return t, ErrUnavailable
	}
	return t, targetValid(t, account)
}
func (c *AccountClient) ResolveEndpoint(ctx context.Context, t PushTarget) (Endpoint, error) {
	var endpoint Endpoint
	if targetValid(t, t.AccountID) != nil || t.Status != "active" {
		return endpoint, ErrForbidden
	}
	e := c.rpc.call(ctx, "POST", accountPath(t.AccountID)+"/targets/"+url.PathEscape(t.DeviceInstallationID)+":resolve", t, &endpoint)
	return endpoint, e
}
func (c *AccountClient) DisableTarget(ctx context.Context, t PushTarget, reason string) error {
	if targetValid(t, t.AccountID) != nil || reason == "" || len(reason) > 160 {
		return ErrInvalid
	}
	input := struct {
		Target PushTarget
		Reason string
	}{t, reason}
	return c.rpc.call(ctx, "POST", accountPath(t.AccountID)+"/targets/"+url.PathEscape(t.DeviceInstallationID)+":disable", input, nil)
}
func (c *AccountClient) DeleteTargets(ctx context.Context, account string) error {
	if account == "" {
		return ErrInvalid
	}
	return c.rpc.call(ctx, "DELETE", accountPath(account)+"/targets", nil, nil)
}

var _ TargetAuthority = (*AccountClient)(nil)
var _ TargetLister = (*AccountClient)(nil)

type RecipientConsumer struct {
	Store     Store
	Authority TargetAuthority
}

func (c RecipientConsumer) EnqueueChat(ctx context.Context, event, account, resource string, created time.Time) (InboxItem, bool, error) {
	if event == "" || account == "" || resource == "" || created.IsZero() || created.After(time.Now().Add(time.Second)) {
		return InboxItem{}, false, ErrInvalid
	}
	item := InboxItem{Identity: Identity{AccountID: account, TriggerEventID: event, TemplateVersion: "chat-message-v2"}, Category: "chat", Summary: "You have a new message", ResourceID: resource, CreatedAt: created, ExpiresAt: created.Add(24 * time.Hour)}
	lister, ok := c.Authority.(TargetLister)
	if !ok {
		return item, false, ErrUnavailable
	}
	targets, e := lister.Targets(ctx, account)
	if e != nil {
		return item, false, e
	}
	attempts := []DeliveryAttempt{}
	for _, t := range targets {
		if targetValid(t, account) != nil {
			return item, false, ErrUnavailable
		}
		if t.Status == "active" {
			attempts = append(attempts, DeliveryAttempt{Channel: "push", TargetID: t.DeviceInstallationID, TargetRevision: t.Revision, ConsentRevision: t.ConsentRevision})
		}
	}
	return c.Store.CommitTrigger(ctx, item, attempts)
}

type ChatClient struct{ rpc accountRPC }

func NewChatClient(base string, client *http.Client, credentials *Credentials, tokenRef string) (*ChatClient, error) {
	rpc, e := newAccountRPC(base, client, credentials, tokenRef)
	if e != nil {
		return nil, e
	}
	return &ChatClient{rpc: rpc}, nil
}
func (c *ChatClient) EnqueueChat(ctx context.Context, event, account, resource string, created time.Time) (string, bool, error) {
	input := ChatRequest{EventID: event, AccountID: account, ResourceID: resource, CreatedAt: created}
	var output ChatResponse
	e := c.rpc.call(ctx, "POST", "/internal/v2/notifications/chat", input, &output)
	if e != nil {
		return "", false, e
	}
	if output.NotificationID == "" {
		return "", false, ErrUnavailable
	}
	return output.NotificationID, output.Created, nil
}

func (c *AccountClient) Ready(ctx context.Context) error {
	if c == nil {
		return ErrUnavailable
	}
	return c.rpc.credentials.Ready(ctx)
}
func (c *ChatClient) Ready(ctx context.Context) error {
	if c == nil {
		return ErrUnavailable
	}
	return c.rpc.credentials.Ready(ctx)
}
func (c *ChatClient) Run(ctx context.Context) error {
	if c == nil {
		return ErrUnavailable
	}
	return c.rpc.credentials.Run(ctx)
}
func (c *AccountClient) Preferences(ctx context.Context, account string) (discovery.PreferenceSnapshot, error) {
	var p discovery.PreferenceSnapshot
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/discovery-preferences", nil, &p)
	if e != nil {
		return p, e
	}
	if p.AccountID != account || p.Revision == 0 || p.ObservedAt.IsZero() || p.ObservedAt.After(time.Now()) || time.Since(p.ObservedAt) > 60*time.Second {
		return p, ErrUnavailable
	}
	return p, nil
}
func (c *AccountClient) Watchlist(ctx context.Context, account string) (discovery.WatchlistSnapshot, error) {
	var p discovery.WatchlistSnapshot
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/watchlist", nil, &p)
	if e != nil {
		return p, e
	}
	if p.AccountID != account || p.Revision == 0 || len(p.Entries) > 1000 {
		return p, ErrUnavailable
	}
	return p, nil
}
func (c *AccountClient) VerifiedAccount(ctx context.Context, account string) (bool, error) {
	var v struct{ Verified bool }
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/verified-account", nil, &v)
	return v.Verified, e
}
func (c *AccountClient) CurrentAttribution(ctx context.Context, account, wallet string) (bool, string, error) {
	var v struct {
		OptedIn   bool
		ProfileID string
	}
	e := c.rpc.call(ctx, "GET", accountPath(account)+"/attribution?wallet="+url.QueryEscape(wallet), nil, &v)
	if e != nil {
		return false, "", e
	}
	return v.OptedIn, v.ProfileID, nil
}

var _ discovery.PreferencesConsumer = (*AccountClient)(nil)
