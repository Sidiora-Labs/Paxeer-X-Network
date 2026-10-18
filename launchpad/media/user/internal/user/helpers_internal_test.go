package user

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/image"
)

func TestNormalizeAddr(t *testing.T) {
	cases := map[string]string{
		"0xABCdef": "0xabcdef",
		"  0xAb ":  "0xab",
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeAddr(in); got != want {
			t.Errorf("normalizeAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	if got := baseURL("https://cdn.test", &http.Request{}); got != "https://cdn.test" {
		t.Errorf("configured baseURL = %q", got)
	}
	r := &http.Request{Host: "api.local", Header: http.Header{}}
	if got := baseURL("", r); got != "http://api.local" {
		t.Errorf("http baseURL = %q", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := baseURL("", r); got != "https://api.local" {
		t.Errorf("forwarded https baseURL = %q", got)
	}
}

func TestBuildImages(t *testing.T) {
	addr := "0xabc"
	imgs := []sqlcdb.UsersUserImage{
		{ImageType: image.TypeAvatar, StorageKey: "avatars/avatar-0xabc.png"},
		{ImageType: image.TypeBanner, StorageKey: "banners/banner-0xabc.webp"},
		{ImageType: "unknown", StorageKey: "x/y.gif"},
	}
	got := buildImages("https://cdn.test", addr, imgs)
	if got.Avatar == nil || *got.Avatar != "https://cdn.test/users/0xabc/avatar" {
		t.Errorf("avatar = %v", got.Avatar)
	}
	if got.Banner == nil || *got.Banner != "https://cdn.test/users/0xabc/banner" {
		t.Errorf("banner = %v", got.Banner)
	}

	empty := buildImages("https://cdn.test", addr, nil)
	if empty.Avatar != nil || empty.Banner != nil {
		t.Errorf("expected nil images, got %+v", empty)
	}
}

func TestBuildCreatedPools(t *testing.T) {
	rows := []sqlcdb.ListCreatedPoolsRow{
		{PoolAddress: "0xp1", TokenAddress: "0xt1", CreatedAt: 100},
		{PoolAddress: "0xp2", TokenAddress: "0xt2", CreatedAt: 200},
	}
	got := buildCreatedPools(rows)
	if len(got) != 2 || got[0].PoolAddress != "0xp1" || got[1].CreatedAt != 200 {
		t.Errorf("buildCreatedPools = %+v", got)
	}
	if buildCreatedPools(nil) == nil {
		t.Error("expected empty slice, got nil")
	}
}

func TestStrToText(t *testing.T) {
	if tx := strToText(""); tx.Valid {
		t.Error("empty string should map to NULL")
	}
	if tx := strToText("  "); tx.Valid {
		t.Error("blank string should map to NULL")
	}
	tx := strToText("hello")
	if !tx.Valid || tx.String != "hello" {
		t.Errorf("strToText(hello) = %+v", tx)
	}
}

func TestTextPtr(t *testing.T) {
	if textPtr(pgtype.Text{Valid: false}) != nil {
		t.Error("NULL text should map to nil")
	}
	p := textPtr(pgtype.Text{String: "x", Valid: true})
	if p == nil || *p != "x" {
		t.Errorf("textPtr = %v", p)
	}
}

func TestInt64Ptr(t *testing.T) {
	if p := int64Ptr(42); p == nil || *p != 42 {
		t.Errorf("int64Ptr = %v", p)
	}
}

func TestItoa64(t *testing.T) {
	if got := itoa64(12345); got != "12345" {
		t.Errorf("itoa64 = %q", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty empty = %q", got)
	}
}
