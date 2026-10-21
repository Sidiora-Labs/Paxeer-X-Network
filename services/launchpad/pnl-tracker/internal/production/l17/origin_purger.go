package l17

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OriginPurger completes card and share purges for a deployment whose public
// origin is not fronted by a caching CDN. The portfolio image and share routes
// answer with Cache-Control: no-store, so with no CDN there is no cached copy
// to invalidate: the purge effect is complete exactly when the public origin
// itself answers 410 Gone for every purged URL, which is what this purger
// verifies (the same post-condition the Cloudflare purgers check after their
// purge_cache call). A URL still served is reported unavailable and the
// worker retries.
type OriginPurger struct {
	PublicOrigin string
	Client       *http.Client
}

// ValidPublicOrigin reports whether origin is an https origin with no user
// info, path, query or fragment.
func ValidPublicOrigin(origin string) bool {
	parsed, e := url.Parse(origin)
	return e == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/")
}

func (p OriginPurger) PurgeCard(ctx context.Context, id string) error {
	if !validUUID(id) {
		return ErrUnavailable
	}
	if !ValidPublicOrigin(p.PublicOrigin) {
		return ErrInvalid
	}
	return p.verifyGone(ctx, 3*time.Second, strings.TrimRight(p.PublicOrigin, "/")+"/v2/portfolio/cards/"+id+"/image.png")
}

func (p OriginPurger) PurgeShare(ctx context.Context, id string) error {
	if !validUUID(id) {
		return ErrUnavailable
	}
	if !ValidPublicOrigin(p.PublicOrigin) {
		return ErrInvalid
	}
	base := strings.TrimRight(p.PublicOrigin, "/") + "/v3/portfolio/shares/" + id
	return p.verifyGone(ctx, 5*time.Second, base, base+"/image.png")
}

func (p OriginPurger) verifyGone(ctx context.Context, timeout time.Duration, targets ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if p.Client != nil {
		client.Transport = p.Client.Transport
	}
	for _, target := range targets {
		check, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if e != nil {
			return e
		}
		actual, e := client.Do(check)
		if e != nil {
			return ErrUnavailable
		}
		actual.Body.Close()
		if actual.StatusCode != http.StatusGone {
			return ErrUnavailable
		}
	}
	return nil
}
