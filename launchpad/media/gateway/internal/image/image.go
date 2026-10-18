// Package image holds the gateway's pure (dependency-free) upload-validation
// helpers: the MIME allowlist and server-side SVG sanitisation. They mirror
// media/metadata's image rules so the edge rejects unsafe uploads BEFORE they
// reach the authoritative writer (defense in depth). Kept pure for exhaustive
// unit testing.
package image

import (
	"regexp"
	"strings"
)

// allowedMimes is the upload MIME allowlist (parity with media/metadata).
var allowedMimes = map[string]struct{}{
	"image/webp": {},
	"image/png":  {},
	"image/jpeg": {},
}

// AllowedMime reports whether mime is an accepted upload content type.
func AllowedMime(mime string) bool {
	_, ok := allowedMimes[strings.ToLower(strings.TrimSpace(mime))]
	return ok
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

// IsSVG reports whether mime is the SVG content type.
func IsSVG(mime string) bool {
	return strings.EqualFold(strings.TrimSpace(mime), "image/svg+xml")
}

// IsSVGSafe reports whether an SVG buffer is free of active content (scripts,
// event handlers, javascript: URIs, iframe/embed/object).
func IsSVGSafe(buf []byte) bool {
	text := string(buf)
	switch {
	case reSvgScript.MatchString(text),
		reSvgOn.MatchString(text),
		reSvgJS.MatchString(text),
		reSvgIframe.MatchString(text),
		reSvgEmbed.MatchString(text),
		reSvgObject.MatchString(text):
		return false
	default:
		return true
	}
}
