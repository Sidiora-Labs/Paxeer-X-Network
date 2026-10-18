package image

import (
	"bytes"
	"errors"
	"image"
	"net/http"
)

// Upload dimension bounds for legacy logo/banner uploads. They match the
// raster pipeline's decode limits (Reencode) so the same pixel budget applies.
const (
	MaxDimension = 8192
	MaxPixels    = 20_000_000
)

// ErrMimeMismatch reports that the declared Content-Type of an upload does not
// match the type sniffed from its bytes.
var ErrMimeMismatch = errors.New("declared content type does not match the image bytes")

// Sniffed is the verified type and size of an uploaded raster.
type Sniffed struct {
	MIME          string
	Width, Height int
}

// Sniff verifies an upload from its bytes, never from client metadata alone:
// the sniffed type must be on the allowlist (webp, png, jpeg), equal the
// declared type, decode as that format, and fit the dimension bounds.
func Sniff(declared string, b []byte) (Sniffed, error) {
	mime := http.DetectContentType(b)
	if !AllowedMime(mime) {
		return Sniffed{}, ErrInvalid
	}
	if !AllowedMime(declared) || ExtForMime(declared) != ExtForMime(mime) {
		return Sniffed{}, ErrMimeMismatch
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return Sniffed{}, ErrInvalid
	}
	if (mime == "image/png" && format != "png") || (mime == "image/jpeg" && format != "jpeg") || (mime == "image/webp" && format != "webp") {
		return Sniffed{}, ErrInvalid
	}
	if c.Width < 1 || c.Height < 1 || c.Width > MaxDimension || c.Height > MaxDimension || int64(c.Width)*int64(c.Height) > MaxPixels {
		return Sniffed{}, ErrLimit
	}
	return Sniffed{MIME: mime, Width: c.Width, Height: c.Height}, nil
}
