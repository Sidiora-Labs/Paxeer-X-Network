package image_test

import (
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
)

func TestAllowedMimeAndExt(t *testing.T) {
	cases := []struct {
		mime    string
		allowed bool
		ext     string
	}{
		{"image/webp", true, "webp"},
		{"image/png", true, "png"},
		{"image/svg+xml", false, ""},
		{"image/jpeg", true, "jpg"},
		{"IMAGE/PNG", true, "png"},     // case-insensitive
		{"  image/png  ", true, "png"}, // trimmed
		{"image/gif", false, ""},       // not allowed
		{"application/pdf", false, ""}, // not allowed
		{"", false, ""},                // empty
	}
	for _, tc := range cases {
		t.Run(tc.mime, func(t *testing.T) {
			if got := image.AllowedMime(tc.mime); got != tc.allowed {
				t.Errorf("AllowedMime(%q) = %v, want %v", tc.mime, got, tc.allowed)
			}
			if got := image.ExtForMime(tc.mime); got != tc.ext {
				t.Errorf("ExtForMime(%q) = %q, want %q", tc.mime, got, tc.ext)
			}
		})
	}
}

func TestAllowedMimeList(t *testing.T) {
	list := image.AllowedMimeList()
	if len(list) != 3 {
		t.Fatalf("AllowedMimeList len = %d, want 3", len(list))
	}
	for _, m := range list {
		if !image.AllowedMime(m) {
			t.Errorf("listed MIME %q not allowed", m)
		}
	}
}

func TestIsSVG(t *testing.T) {
	if !image.IsSVG("image/svg+xml") {
		t.Error("IsSVG(image/svg+xml) = false, want true")
	}
	if !image.IsSVG("IMAGE/SVG+XML") {
		t.Error("IsSVG is not case-insensitive")
	}
	if image.IsSVG("image/png") {
		t.Error("IsSVG(image/png) = true, want false")
	}
}

func TestIsSVGSafe(t *testing.T) {
	cases := []struct {
		name string
		svg  string
		safe bool
	}{
		{"clean", `<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`, true},
		{"script tag", `<svg><script>alert(1)</script></svg>`, false},
		{"script self-close", `<svg><script src="x"/></svg>`, false},
		{"event handler", `<svg onload="alert(1)"></svg>`, false},
		{"onclick", `<svg><rect onclick="x()"/></svg>`, false},
		{"javascript uri", `<svg><a href="javascript:alert(1)"/></svg>`, false},
		{"iframe", `<svg><iframe src="x"></iframe></svg>`, false},
		{"embed", `<svg><embed src="x"></svg>`, false},
		{"object", `<svg><object data="x"></object></svg>`, false},
		{"uppercase script", `<svg><SCRIPT>x</SCRIPT></svg>`, false},
		{"empty", ``, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := image.IsSVGSafe([]byte(tc.svg)); got != tc.safe {
				t.Errorf("IsSVGSafe(%q) = %v, want %v", tc.svg, got, tc.safe)
			}
		})
	}
}
