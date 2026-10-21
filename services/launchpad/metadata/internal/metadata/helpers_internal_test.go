package metadata

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
)

func TestNormalizeAddr(t *testing.T) {
	if got := normalizeAddr("  0xABCdef  "); got != "0xabcdef" {
		t.Errorf("normalizeAddr = %q", got)
	}
}

func TestStripExt(t *testing.T) {
	cases := map[string]string{
		"0xabc.png":  "0xabc",
		"0xabc.json": "0xabc",
		"0xabc":      "0xabc",
		"a.b.c":      "a.b",
	}
	for in, want := range cases {
		if got := stripExt(in); got != want {
			t.Errorf("stripExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtFromKey(t *testing.T) {
	cases := map[string]string{
		"logos/logo-0x.png":      "png",
		"banners/banner-0x.webp": "webp",
		"noext":                  "png", // default
		"trailingdot.":           "png", // default (nothing after dot)
	}
	for in, want := range cases {
		if got := extFromKey(in); got != want {
			t.Errorf("extFromKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/x", nil)
	if got := baseURL("https://cdn.test/", r); got != "https://cdn.test/" {
		t.Errorf("baseURL(public) = %q", got)
	}
	// Empty PublicURL falls back to request host.
	if got := baseURL("", r); got != "http://example.com" {
		t.Errorf("baseURL(host) = %q", got)
	}
	// X-Forwarded-Proto promotes to https.
	r2 := httptest.NewRequest("GET", "http://example.com/x", nil)
	r2.Header.Set("X-Forwarded-Proto", "https")
	if got := baseURL("", r2); got != "https://example.com" {
		t.Errorf("baseURL(xfp) = %q", got)
	}
}

func TestParseBatchAddresses(t *testing.T) {
	in := []string{"0x" + repeat("a", 40) + ",not-addr", "0x" + repeat("a", 40), "0x" + repeat("B", 40)}
	got := parseBatchAddresses(in)
	want := []string{"0x" + repeat("a", 40), "0x" + repeat("b", 40)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseBatchAddresses = %v, want %v", got, want)
	}
}

func TestParseBatchAddresses_Cap(t *testing.T) {
	var in []string
	for i := 0; i < maxBatch+50; i++ {
		// distinct valid addresses
		in = append(in, "0x"+pad(i))
	}
	if got := parseBatchAddresses(in); len(got) != maxBatch {
		t.Errorf("cap len = %d, want %d", len(got), maxBatch)
	}
}

func TestParseTags(t *testing.T) {
	cases := []struct {
		name string
		in   pgtype.Text
		want []string
	}{
		{"null", pgtype.Text{Valid: false}, []string{}},
		{"empty string", pgtype.Text{String: "  ", Valid: true}, []string{}},
		{"valid", pgtype.Text{String: `["a","b"]`, Valid: true}, []string{"a", "b"}},
		{"malformed", pgtype.Text{String: `{bad`, Valid: true}, []string{}},
		{"json null", pgtype.Text{String: `null`, Valid: true}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseTags(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseTags = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStrToTextAndTextPtr(t *testing.T) {
	if tx := strToText(""); tx.Valid {
		t.Error("strToText(\"\") should be NULL")
	}
	if tx := strToText("x"); !tx.Valid || tx.String != "x" {
		t.Errorf("strToText(x) = %+v", tx)
	}
	if textPtr(pgtype.Text{Valid: false}) != nil {
		t.Error("textPtr(NULL) should be nil")
	}
	if p := textPtr(pgtype.Text{String: "y", Valid: true}); p == nil || *p != "y" {
		t.Errorf("textPtr = %v", p)
	}
}

func TestBuildImages(t *testing.T) {
	imgs := []sqlcdb.MetadataTokenImage{
		{ImageType: "logo", StorageKey: "logos/logo-0x.png"},
		{ImageType: "banner", StorageKey: "banners/banner-0x.webp"},
		{ImageType: "other", StorageKey: "x.png"}, // ignored
	}
	got := buildImages("https://cdn", "0xabc", imgs)
	if got.Logo == nil || *got.Logo != "https://cdn/logo/0xabc.png" {
		t.Errorf("logo = %v", got.Logo)
	}
	if got.Banner == nil || *got.Banner != "https://cdn/banner/0xabc.webp" {
		t.Errorf("banner = %v", got.Banner)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// pad returns a 40-hex-char string encoding i (distinct per i, lowercase).
func pad(i int) string {
	const hexd = "0123456789abcdef"
	b := make([]byte, 40)
	for j := range b {
		b[j] = '0'
	}
	k := len(b) - 1
	for i > 0 && k >= 0 {
		b[k] = hexd[i&0xf]
		i >>= 4
		k--
	}
	return string(b)
}
