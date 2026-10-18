package image_test

import (
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/image"
)

func TestAllowedMime(t *testing.T) {
	cases := []struct {
		mime string
		want bool
	}{
		{"image/webp", true},
		{"image/png", true},
		{"image/svg+xml", true},
		{"image/jpeg", true},
		{"IMAGE/PNG", true},      // case-insensitive
		{"  image/jpeg  ", true}, // trimmed
		{"image/gif", false},
		{"application/json", false},
		{"", false},
	}
	for _, c := range cases {
		if got := image.AllowedMime(c.mime); got != c.want {
			t.Errorf("AllowedMime(%q) = %v, want %v", c.mime, got, c.want)
		}
	}
}

func TestExtForMime(t *testing.T) {
	cases := map[string]string{
		"image/webp":    "webp",
		"image/png":     "png",
		"image/svg+xml": "svg",
		"image/jpeg":    "jpg",
		"IMAGE/JPEG":    "jpg",
		"image/gif":     "",
	}
	for mime, want := range cases {
		if got := image.ExtForMime(mime); got != want {
			t.Errorf("ExtForMime(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestAllowedMimeList(t *testing.T) {
	list := image.AllowedMimeList()
	if len(list) != 4 {
		t.Fatalf("len = %d, want 4", len(list))
	}
	for _, m := range list {
		if !image.AllowedMime(m) {
			t.Errorf("listed mime %q not allowed", m)
		}
	}
}

func TestIsValidType(t *testing.T) {
	cases := map[string]bool{
		image.TypeAvatar: true,
		image.TypeBanner: true,
		"logo":           false,
		"":               false,
		"Avatar":         false,
	}
	for typ, want := range cases {
		if got := image.IsValidType(typ); got != want {
			t.Errorf("IsValidType(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestIsSVG(t *testing.T) {
	if !image.IsSVG("image/svg+xml") || !image.IsSVG("  IMAGE/SVG+XML ") {
		t.Error("expected SVG mime to match")
	}
	if image.IsSVG("image/png") {
		t.Error("png should not be SVG")
	}
}

func TestIsSVGSafe(t *testing.T) {
	safe := []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>`,
		`<svg><rect width="10" height="10" fill="blue"/></svg>`,
	}
	for _, s := range safe {
		if !image.IsSVGSafe([]byte(s)) {
			t.Errorf("expected safe: %q", s)
		}
	}
	unsafe := []string{
		`<svg><script>alert(1)</script></svg>`,
		`<svg onload="alert(1)"></svg>`,
		`<svg><a href="javascript:alert(1)">x</a></svg>`,
		`<svg><iframe src="x"></iframe></svg>`,
		`<svg><embed src="x"></svg>`,
		`<svg><object data="x"></object></svg>`,
	}
	for _, s := range unsafe {
		if image.IsSVGSafe([]byte(s)) {
			t.Errorf("expected unsafe: %q", s)
		}
	}
}
