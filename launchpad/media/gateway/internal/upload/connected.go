package upload

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/proxy"
	gatewaystorage "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

var connectedAssetPath = regexp.MustCompile(`^/v2/(tokens|accounts)/0x[0-9a-f]{40}/assets/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type ConnectedDeps struct {
	Upstream              *proxy.Connected
	Identity              proxy.RequestIdentityAuthority
	Admission             *Admission
	IngressBytesPerSecond int64
	Observer              *gatewaystorage.MediaObserver
}
type Connected struct {
	upstream       *proxy.Connected
	identity       proxy.RequestIdentityAuthority
	admission      *Admission
	slots          chan struct{}
	bytesPerSecond int64
	observer       *gatewaystorage.MediaObserver
}

func NewConnected(d ConnectedDeps) (*Connected, error) {
	if d.Upstream == nil || d.Admission == nil || d.Identity == nil || d.IngressBytesPerSecond < 32768 || d.IngressBytesPerSecond > 1<<30 {
		return nil, errors.New("upload dependencies unavailable")
	}
	return &Connected{d.Upstream, d.Identity, d.Admission, make(chan struct{}, d.Admission.global), d.IngressBytesPerSecond, d.Observer}, nil
}
func (h *Connected) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.observer != nil {
		observed := &uploadResponse{ResponseWriter: w}
		w = observed
		defer func() {
			outcome := "unavailable"
			switch observed.status {
			case 200:
				outcome = "approved"
			case 202:
				outcome = "quarantined"
			case 401, 403:
				outcome = "unauthenticated"
			case 429:
				outcome = "rate_limited"
			case 400, 408, 413, 415, 422:
				outcome = "rejected"
			}
			_ = h.observer.RecordUpload(outcome)
		}()
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL == nil || r.URL.RawPath != "" || r.Method != "PUT" || !connectedAssetPath.MatchString(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	identityContext, identityCancel := context.WithTimeout(r.Context(), 3*time.Second)
	p, e := h.identity.AuthenticateRequest(identityContext, r)
	identityCancel()
	if e != nil {
		status := http.StatusServiceUnavailable
		var authority *sharedauth.IdentityAuthorityError
		if errors.As(e, &authority) {
			status = authority.StatusCode()
		} else if errors.Is(e, sharedauth.ErrScopeDenied) {
			status = 403
		}
		http.Error(w, "upload identity unavailable", status)
		return
	}
	if !walletPattern.MatchString(p.Wallet) {
		http.Error(w, "upload identity unavailable", 403)
		return
	}
	if r.ContentLength > 6<<20 {
		http.Error(w, "upload exceeds size limit", 413)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "upload busy", 503)
		return
	}
	release, e := h.admission.Acquire(r.Context(), p.Wallet)
	if e != nil {
		w.Header().Set("Retry-After", "60")
		status := 503
		if errors.Is(e, ErrBusy) {
			status = 429
		}
		http.Error(w, "upload admission unavailable", status)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	now := time.Now()
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(now.Add(5 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	r.Body = http.MaxBytesReader(w, &aggregateIngressReader{ReadCloser: &ingressReader{ReadCloser: r.Body, controller: controller, window: now, deadline: now.Add(30 * time.Second)}, ctx: ctx, db: h.admission.db, key: h.admission.ns + ":upload:bytes", limit: h.bytesPerSecond}, 6<<20)
	h.upstream.ServeHTTP(w, r)
}

type aggregateIngressReader struct {
	io.ReadCloser
	ctx   context.Context
	db    *redis.Client
	key   string
	limit int64
}

func (r *aggregateIngressReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 32768 {
		buffer = buffer[:32768]
	}
	reserved := int64(len(buffer))
	if reserved == 0 {
		return 0, nil
	}
	for {
		permitted, e := r.db.Eval(r.ctx, `local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000);local last=tonumber(redis.call('HGET',KEYS[1],'time') or now);local cap=tonumber(ARGV[1]);local tokens=tonumber(redis.call('HGET',KEYS[1],'tokens') or cap);tokens=math.min(cap,tokens+math.max(0,now-last)*cap/1000);if tokens<tonumber(ARGV[2]) then redis.call('HSET',KEYS[1],'time',now,'tokens',tokens);redis.call('PEXPIRE',KEYS[1],2000);return 0 end;redis.call('HSET',KEYS[1],'time',now,'tokens',tokens-tonumber(ARGV[2]));redis.call('PEXPIRE',KEYS[1],2000);return 1`, []string{r.key}, r.limit, reserved).Int()
		if e != nil {
			return 0, ErrBusy
		}
		if permitted == 1 {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return 0, r.ctx.Err()
		case <-timer.C:
		}
	}
	return r.ReadCloser.Read(buffer)
}

type uploadResponse struct {
	http.ResponseWriter
	status int
}

func (w *uploadResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *uploadResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(data)
}
func (w *uploadResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
