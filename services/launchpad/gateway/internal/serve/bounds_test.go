package serve

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Adversarial origin fixture uses a real HTTP body but intentionally lies about
// object metadata, modeling a corrupt adapter/provider rather than approval.
type httpObject struct {
	url  string
	size int64
	mime string
}

func (o httpObject) Get(ctx context.Context, key string) (*storage.Object, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", o.url, nil)
	if e != nil {
		return nil, e
	}
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		return nil, e
	}
	return &storage.Object{Body: resp.Body, Size: o.size, ContentType: o.mime, ETag: "owned"}, nil
}
func TestOriginActualBytesCannotBypassDeclaredBounds(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", 10000))) }))
	defer up.Close()
	h := New(Deps{Buckets: map[string]Reader{"token": httpObject{up.URL, 10, "image/png"}}})
	w := do(t, h, "GET", "/media/token/approved-object", nil)
	if w.Code != 200 || w.Body.Len() != 10 {
		t.Fatal(w.Code, w.Body.Len())
	}
	h = New(Deps{Buckets: map[string]Reader{"token": httpObject{up.URL, 10000, "text/html"}}})
	w = do(t, h, "GET", "/media/token/approved-object", nil)
	if w.Code != 502 {
		t.Fatal("unsafe origin MIME", w.Code)
	}
}
