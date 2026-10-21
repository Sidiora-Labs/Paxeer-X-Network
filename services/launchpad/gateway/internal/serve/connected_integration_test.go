package serve

import (
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"strings"
	"testing"

	gatewaystorage "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
)

func TestConnectedDerivativeNamespace(t *testing.T) {
	hash := strings.Repeat("a", 64)
	if !gatewaystorage.ValidDeliveryKey("approved/" + hash + "/256/" + strings.Repeat("b", 64) + ".png") {
		t.Fatal("real metadata derivative namespace rejected")
	}
	for _, key := range []string{"private/" + hash, "approved/" + hash + "/../original", "approved/" + hash + "/256/%2e%2e", "approved/" + hash + "/2048/" + hash + ".png", "approved/" + hash + "/256/" + hash + ".svg"} {
		if gatewaystorage.ValidDeliveryKey(key) {
			t.Fatal("unsafe namespace admitted", key)
		}
	}
}

func TestConnectedEncodedTraversalNeverReachesApproval(t *testing.T) {
	router := chi.NewRouter()
	NewApproved(Deps{}, nil).RegisterRoutes(router)
	for _, path := range []string{"/media/token/approved/%2e%2e/private/original.png", "/media/token/approved/%252e%252e/private/original.png", "/media/token/approved/%2fprivate/original.png", "/media/token/approved/%5cprivate/original.png"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || w.Body.String() == "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("CDN-Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Header())
		}
	}
}
