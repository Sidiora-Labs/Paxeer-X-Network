package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Credentials struct {
	mu        sync.RWMutex
	values    map[string]string
	provider  secrets.Provider
	refreshed time.Time
	ready     bool
}

func NewCredentials(ctx context.Context, options secrets.Options, keys ...string) (*Credentials, error) {
	options.CacheTTL = 0
	p, e := secrets.NewChecked(options)
	if e != nil {
		return nil, e
	}
	c := &Credentials{provider: p, values: map[string]string{}}
	for _, key := range keys {
		if key == "" {
			return nil, ErrInvalid
		}
		value, e := p.GetRequired(ctx, key)
		if e != nil {
			return nil, ErrUnavailable
		}
		c.values[key] = value
	}
	c.ready = true
	c.refreshed = time.Now()
	return c, nil
}
func (c *Credentials) Get(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready || time.Since(c.refreshed) > 60*time.Second {
		return "", ErrUnavailable
	}
	value, ok := c.values[key]
	if !ok {
		return "", ErrUnavailable
	}
	return value, nil
}
func (c *Credentials) Refresh(ctx context.Context) error {
	c.mu.RLock()
	keys := []string{}
	for k := range c.values {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	next := map[string]string{}
	for _, k := range keys {
		value, e := c.provider.GetRequired(ctx, k)
		if e != nil || value == "" {
			c.mu.Lock()
			c.ready = false
			c.mu.Unlock()
			return ErrUnavailable
		}
		next[k] = value
	}
	c.mu.Lock()
	c.values = next
	c.refreshed = time.Now()
	c.ready = true
	c.mu.Unlock()
	return nil
}
func (c *Credentials) Run(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			refresh, cancel := context.WithTimeout(ctx, 5*time.Second)
			e := c.Refresh(refresh)
			cancel()
			if e != nil {
				return e
			}
		}
	}
}
func (c *Credentials) Ready(ctx context.Context) error {
	_, _ = ctx.Deadline()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready || time.Since(c.refreshed) > 60*time.Second {
		return ErrUnavailable
	}
	return nil
}

type RuntimeOptions struct {
	Secrets                                                                                                     secrets.Options
	APNSKey, APNSKeyID, APNSTeamID, APNSTopic, FCMServiceAccount, VAPIDKey, VAPIDSubject, Owner, DeepLinkOrigin string
	APNSSandbox                                                                                                 bool
	EnableAPNS, EnableFCM, EnableWebPush                                                                        bool
}
type PushRuntime struct {
	Credentials *Credentials
	Worker      Worker
}

func NewPushRuntime(ctx context.Context, db *sql.DB, authority TargetAuthority, o RuntimeOptions) (*PushRuntime, error) {
	if db == nil || authority == nil || o.Owner == "" {
		return nil, ErrInvalid
	}
	u, e := url.Parse(o.DeepLinkOrigin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrInvalid
	}
	if !o.EnableAPNS && (o.APNSKey != "" || o.APNSKeyID != "" || o.APNSTeamID != "" || o.APNSTopic != "") {
		return nil, ErrInvalid
	}
	if !o.EnableFCM && o.FCMServiceAccount != "" {
		return nil, ErrInvalid
	}
	if !o.EnableWebPush && (o.VAPIDKey != "" || o.VAPIDSubject != "") {
		return nil, ErrInvalid
	}
	refs := []string{}
	if o.EnableAPNS {
		refs = append(refs, o.APNSKey, o.APNSKeyID, o.APNSTeamID, o.APNSTopic)
	}
	if o.EnableFCM {
		refs = append(refs, o.FCMServiceAccount)
	}
	if o.EnableWebPush {
		refs = append(refs, o.VAPIDKey, o.VAPIDSubject)
	}
	credentials, e := NewCredentials(ctx, o.Secrets, refs...)
	if e != nil {
		return nil, e
	}
	adapters := map[string]ChannelAdapter{}
	if o.EnableAPNS {
		a, e := NewAPNS(credentials, authority, o.APNSKey, o.APNSKeyID, o.APNSTeamID, o.APNSTopic, o.APNSSandbox)
		if e != nil {
			return nil, e
		}
		adapters[a.Provider()] = a
	}
	if o.EnableFCM {
		a, e := NewFCM(credentials, authority, o.FCMServiceAccount)
		if e != nil {
			return nil, e
		}
		adapters[a.Provider()] = a
	}
	if o.EnableWebPush {
		a, e := NewWebPush(credentials, authority, o.VAPIDKey, o.VAPIDSubject)
		if e != nil {
			return nil, e
		}
		adapters[a.Provider()] = a
	}
	return &PushRuntime{Credentials: credentials, Worker: Worker{Store: Store{DB: db}, Authority: authority, Adapters: adapters, Owner: o.Owner, DeepLinkOrigin: o.DeepLinkOrigin}}, nil
}
func (r *PushRuntime) Run(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- r.Credentials.Run(run) }()
	go func() { results <- r.Worker.Run(run) }()
	e := <-results
	cancel()
	<-results
	return e
}

type RuntimeConfig struct {
	Secrets                                                                                                                      secrets.Options
	Client                                                                                                                       *http.Client
	D11Base, D11TokenRef, D12Base, D12TokenRef, PnLBase, PnLTokenRef, CursorKeyRef, InternalTokenRef, DeletePeerDNS, ChatPeerDNS string
	Push                                                                                                                         RuntimeOptions
	Pools                                                                                                                        discovery.SourceAuthority
	Intervals                                                                                                                    discovery.IntervalAuthority
}
type HTTPRuntime struct {
	DB            *sql.DB
	Credentials   *Credentials
	Accounts      *AccountClient
	Push          *PushRuntime
	Discovery     discovery.HTTP
	Notifications HTTP
	Events        discovery.Runtime
	Config        RuntimeConfig
	mu            sync.RWMutex
	running       bool
	failure       error
}

func NewHTTPRuntime(ctx context.Context, db *sql.DB, auth func(*http.Request) (string, error), config RuntimeConfig) (*HTTPRuntime, error) {
	if db == nil || auth == nil || config.Client == nil || config.D11Base == "" || config.D12Base == "" || config.D11TokenRef == "" || config.D12TokenRef == "" || config.CursorKeyRef == "" || config.InternalTokenRef == "" || config.DeletePeerDNS == "" || config.ChatPeerDNS == "" {
		return nil, ErrInvalid
	}
	if config.PnLBase != "" || config.PnLTokenRef != "" {
		return nil, ErrInvalid
	}
	refs := []string{config.D11TokenRef, config.D12TokenRef, config.CursorKeyRef, config.InternalTokenRef}
	credentials, e := NewCredentials(ctx, config.Secrets, refs...)
	if e != nil {
		return nil, e
	}
	key, e := credentials.Get(config.CursorKeyRef)
	if e != nil || len(key) < 32 {
		return nil, ErrInvalid
	}
	accounts, e := NewAccountClient(config.D11Base, config.Client, credentials, config.D11TokenRef)
	if e != nil {
		return nil, e
	}
	socialRPC, e := newAccountRPC(config.D12Base, config.Client, credentials, config.D12TokenRef)
	if e != nil {
		return nil, e
	}
	ds := discovery.Store{DB: db}
	ns := Store{DB: db}
	social := socialAuthority{rpc: socialRPC, store: ds}
	external := sourceAuthority{pools: config.Pools, accounts: accounts, social: social}
	callouts := discovery.CalloutService{Store: ds, Authority: external}
	owned := discovery.OwnedSources{External: external, Callouts: callouts}
	intervals := config.Intervals
	policy := InternalPolicy{Credentials: credentials, TokenRef: config.InternalTokenRef, DeletePeerDNS: config.DeletePeerDNS, ChatPeerDNS: config.ChatPeerDNS}
	r := &HTTPRuntime{DB: db, Credentials: credentials, Accounts: accounts, Config: config}
	r.Discovery = discovery.HTTP{Store: ds, Callouts: callouts, Authenticate: discovery.Authenticate(auth), Intervals: intervals, Feed: discovery.Feed{Store: ds, Sources: owned, Preferences: accounts, Graph: social, Signer: discovery.CursorSigner{Key: []byte(key)}}}
	r.Notifications = HTTP{Store: ns, Discovery: ds, Authenticate: Authenticate(auth), Recipient: RecipientConsumer{Store: ns, Authority: accounts}, InternalAuthorize: policy.Authorize}
	r.Events = discovery.Runtime{Store: ds, Publisher: eventPublisher{triggers: TriggerConsumer{Store: ns, Authority: accounts}, social: social}}
	if config.Push.EnableAPNS || config.Push.EnableFCM || config.Push.EnableWebPush {
		config.Push.Secrets = config.Secrets
		r.Push, e = NewPushRuntime(ctx, db, accounts, config.Push)
		if e != nil {
			return nil, e
		}
	}
	return r, nil
}
func (r *HTTPRuntime) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v2/discovery/notifications", r.Notifications.Handler())
	mux.Handle("/v2/discovery/notifications/", r.Notifications.Handler())
	mux.Handle("/v2/discovery/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.Config.Pools == nil && (strings.HasPrefix(req.URL.Path, "/v2/discovery/feed") || strings.HasPrefix(req.URL.Path, "/v2/discovery/callouts")) {
			respond(w, nil, ErrUnavailable)
			return
		}
		key, e := r.Credentials.Get(r.Config.CursorKeyRef)
		if e != nil || len(key) < 32 {
			respond(w, nil, ErrUnavailable)
			return
		}
		h := r.Discovery
		h.Feed.Signer = discovery.CursorSigner{Key: []byte(key)}
		h.Handler().ServeHTTP(w, req)
	}))
	return mux
}
func (r *HTTPRuntime) InternalHandler() http.Handler { return r.Notifications.InternalHandler() }
func (r *HTTPRuntime) Ready(ctx context.Context) error {
	if r == nil || r.DB == nil {
		return ErrUnavailable
	}
	r.mu.RLock()
	running, failure := r.running, r.failure
	r.mu.RUnlock()
	if !running || failure != nil || r.Config.Pools == nil || r.Discovery.Intervals == nil {
		return ErrUnavailable
	}
	if e := r.Credentials.Ready(ctx); e != nil {
		return e
	}
	if r.Push != nil {
		if e := r.Push.Credentials.Ready(ctx); e != nil {
			return e
		}
	}
	var exists bool
	e := r.DB.QueryRowContext(ctx, `SELECT to_regclass('narrative_discovery.sources') IS NOT NULL AND to_regclass('discovery_notifications.inbox') IS NOT NULL`).Scan(&exists)
	if e != nil {
		return e
	}
	if !exists {
		return ErrUnavailable
	}
	return nil
}
func (r *HTTPRuntime) Run(ctx context.Context) error {
	if r == nil {
		return ErrInvalid
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrInvalid
	}
	r.running = true
	r.failure = nil
	r.mu.Unlock()
	jobs := []func(context.Context) error{r.Credentials.Run, r.Events.Run, r.maintain}
	if r.Push != nil {
		jobs = append(jobs, r.Push.Run)
	}
	results := make(chan error, len(jobs))
	for _, job := range jobs {
		job := job
		go func() { results <- job(run) }()
	}
	e := <-results
	cancel()
	for remaining := 1; remaining < len(jobs); remaining++ {
		<-results
	}
	r.mu.Lock()
	r.running = false
	r.failure = e
	r.mu.Unlock()
	return e
}
func (r *HTTPRuntime) maintain(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		e := r.refreshSources(bounded)
		if e == nil {
			_, e = r.Notifications.Store.Retain(bounded, time.Now().UTC(), 90, 1000)
		}
		if e == nil {
			e = r.Discovery.Store.ExpireSnapshots(bounded, time.Now().UTC())
		}
		cancel()
		if e != nil && !errors.Is(e, ErrUnavailable) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type publicSourceLister interface {
	PublicSources(context.Context) ([]discovery.SourceRef, error)
}

func (r *HTTPRuntime) refreshSources(ctx context.Context) error {
	refs := []discovery.SourceRef{}
	if provider, ok := r.Config.Pools.(publicSourceLister); ok {
		current, e := provider.PublicSources(ctx)
		if e != nil {
			return e
		}
		if len(current) > 2000 {
			return ErrInvalid
		}
		refs = append(refs, current...)
	}
	rows, e := r.DB.QueryContext(ctx, `SELECT document FROM narrative_discovery.sources ORDER BY published_at DESC,event_id LIMIT 2000`)
	if e != nil {
		return e
	}
	for rows.Next() {
		var raw []byte
		var ref discovery.SourceRef
		if e = rows.Scan(&raw); e == nil {
			e = json.Unmarshal(raw, &ref)
		}
		if e != nil {
			rows.Close()
			return e
		}
		refs = append(refs, ref)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		if ref.Tombstone {
			continue
		}
		resource, e := r.Discovery.Feed.Sources.ResolvePublic(ctx, ref.Kind, ref.ID)
		if e != nil {
			return e
		}
		event := discovery.PublicEvent{Consumer: "authority-refresh-v2", EventID: resource.Ref.EventID + ":" + time.Now().UTC().Format(time.RFC3339Nano), Source: resource.Ref, ObservedAt: time.Now().UTC()}
		if e = r.Discovery.Store.Consume(ctx, r.Discovery.Feed.Sources, event); e != nil {
			return e
		}
	}
	return nil
}
