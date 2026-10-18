package policy

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestPublicEgressRejectsMetadataLinkLocalBeforeConnect(t *testing.T) {
	u, err := url.Parse("https://169.254.169.254/latest/meta-data")
	if err != nil {
		t.Fatal(err)
	}
	if PublicURL(u, map[string]bool{"metadata.example": true}) == nil {
		t.Fatal("redirect target accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := DialPublic(ctx, "tcp", "169.254.169.254:443"); err == nil {
		conn.Close()
		t.Fatal("link-local connected")
	}
	if _, err := EgressClient([]EgressDestination{{Name: "rpc", Hostname: "internal.example", Authority: "chain", InternalChain: true}}); err == nil {
		t.Fatal("internal RPC admitted to public-content transport")
	}
}

func TestRedirectAndTranslatedPrivateAddressesFailClosed(t *testing.T) {
	client := PublicClient(map[string]bool{"metadata.example": true, "169.254.169.254": true})
	original, _ := http.NewRequest("GET", "https://metadata.example/image", nil)
	redirect, _ := http.NewRequest("GET", "https://169.254.169.254/latest", nil)
	if err := client.CheckRedirect(redirect, []*http.Request{original}); err == nil {
		t.Fatal("allowlisted link-local redirect admitted")
	}
	for _, address := range []string{"64:ff9b::a00:1", "2002:7f00:1::", "2001:0:4136:e378:8000:63bf:3fff:fdd2"} {
		if publicIP(net.ParseIP(address)) {
			t.Fatal("translated private route admitted", address)
		}
	}
	for _, count := range []int{3, 4} {
		target, _ := http.NewRequest("GET", "https://metadata.example/image", nil)
		err := client.CheckRedirect(target, make([]*http.Request, count))
		if (err == nil) != (count == 3) {
			t.Fatal("redirect count", count, err)
		}
	}
}
