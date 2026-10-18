package image

import "testing"

func TestAllowedMime(t *testing.T) {
	for _, ok := range []string{"image/png", "image/webp", "image/jpeg", " IMAGE/PNG "} {
		if !AllowedMime(ok) {
			t.Errorf("AllowedMime(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"image/svg+xml", "image/gif", "application/json", "", "text/html"} {
		if AllowedMime(bad) {
			t.Errorf("AllowedMime(%q) = true, want false", bad)
		}
	}
}

func TestAllowedMimeList(t *testing.T) {
	if len(AllowedMimeList()) != 3 {
		t.Fatalf("AllowedMimeList len = %d, want 3", len(AllowedMimeList()))
	}
}

func TestIsSVG(t *testing.T) {
	if !IsSVG("image/svg+xml") || !IsSVG(" image/svg+xml ") {
		t.Error("IsSVG should accept the svg content type")
	}
	if IsSVG("image/png") {
		t.Error("IsSVG should reject png")
	}
}

func TestIsSVGSafe(t *testing.T) {
	safe := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	if !IsSVGSafe(safe) {
		t.Error("clean svg should be safe")
	}
	unsafe := [][]byte{
		[]byte(`<svg><script>alert(1)</script></svg>`),
		[]byte(`<svg onload="x()"></svg>`),
		[]byte(`<svg><a href="javascript:x"></a></svg>`),
		[]byte(`<svg><iframe src="x"></iframe></svg>`),
		[]byte(`<svg><embed src="x"></embed></svg>`),
		[]byte(`<svg><object data="x"></object></svg>`),
	}
	for i, u := range unsafe {
		if IsSVGSafe(u) {
			t.Errorf("unsafe svg #%d marked safe", i)
		}
	}
}
