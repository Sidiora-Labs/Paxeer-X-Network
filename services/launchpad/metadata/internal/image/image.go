// Package image holds the pure (dependency-free) image validation helpers for
// uploads: the MIME allowlist, MIME->extension mapping, and server-side SVG
// sanitisation. Kept pure so it is exhaustively unit-tested (100% target) and
// reused by media/metadata + media/user + media/gateway.
package image

import (
	"regexp"
	"strings"
)

// Image type discriminators stored in token_images.image_type / user_images.
const (
	TypeLogo   = "logo"
	TypeBanner = "banner"
)

// allowedMimes is the upload MIME allowlist (parity with the TS allowedMimes).
var allowedMimes = map[string]string{
	"image/webp": "webp",
	"image/png":  "png",
	"image/jpeg": "jpg",
}

// AllowedMime reports whether mime is an accepted upload content type.
func AllowedMime(mime string) bool {
	_, ok := allowedMimes[strings.ToLower(strings.TrimSpace(mime))]
	return ok
}

// ExtForMime returns the canonical file extension for an allowed MIME, or ""
// when the MIME is not allowed.
func ExtForMime(mime string) string {
	return allowedMimes[strings.ToLower(strings.TrimSpace(mime))]
}

// AllowedMimeList returns the accepted MIME types (for error messages).
func AllowedMimeList() []string {
	return []string{"image/webp", "image/png", "image/jpeg"}
}

var (
	reSvgScript = regexp.MustCompile(`(?i)<script[\s>]`)
	reSvgOn     = regexp.MustCompile(`(?i)on\w+\s*=`)
	reSvgJS     = regexp.MustCompile(`(?i)javascript:`)
	reSvgIframe = regexp.MustCompile(`(?i)<iframe[\s>]`)
	reSvgEmbed  = regexp.MustCompile(`(?i)<embed[\s>]`)
	reSvgObject = regexp.MustCompile(`(?i)<object[\s>]`)
)

// IsSVGSafe reports whether an SVG buffer is free of active content (scripts,
// event handlers, javascript: URIs, iframe/embed/object). Mirrors the TS
// isSvgSafe check (3.7). Non-SVG callers should not invoke this.
func IsSVGSafe(buf []byte) bool {
	text := string(buf)
	switch {
	case reSvgScript.MatchString(text):
		return false
	case reSvgOn.MatchString(text):
		return false
	case reSvgJS.MatchString(text):
		return false
	case reSvgIframe.MatchString(text):
		return false
	case reSvgEmbed.MatchString(text):
		return false
	case reSvgObject.MatchString(text):
		return false
	default:
		return true
	}
}

// IsSVG reports whether mime is the SVG content type.
func IsSVG(mime string) bool {
	return strings.EqualFold(strings.TrimSpace(mime), "image/svg+xml")
}
