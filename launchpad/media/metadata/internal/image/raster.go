package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
)

const MaxBytes = 6 << 20

var ErrInvalid = errors.New("unsupported or invalid raster image")
var ErrLimit = errors.New("image resource limit")
var ErrUnavailable = errors.New("image codec unavailable")

type Raster struct {
	MIME          string
	Width, Height int
	Variants      map[int][]byte
}

// ReadBounded admits bytes before full allocation. WebP remains unavailable until
// an isolated qualified decoder is wired; no original-byte fallback is allowed.
func ReadBounded(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > MaxBytes {
		return nil, ErrLimit
	}
	return b, nil
}
func Reencode(ctx context.Context, b []byte) (Raster, error) { return reencodeRaster(ctx, b, false) }
func ReencodeSupported(ctx context.Context, b []byte) (Raster, error) {
	return reencodeRaster(ctx, b, true)
}
func reencodeRaster(ctx context.Context, b []byte, webp bool) (Raster, error) {
	if len(b) > MaxBytes {
		return Raster{}, ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return Raster{}, err
	}
	mime := http.DetectContentType(b)
	if mime == "image/webp" && !webp {
		return Raster{}, ErrUnavailable
	}
	if mime == "image/webp" {
		if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
			return Raster{}, ErrInvalid
		}
		for p := 12; p < len(b); {
			if p+8 > len(b) {
				return Raster{}, ErrInvalid
			}
			n := uint64(binary.LittleEndian.Uint32(b[p+4 : p+8]))
			if n > uint64(len(b)-p-8) {
				return Raster{}, ErrInvalid
			}
			kind := string(b[p : p+4])
			if kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && (n < 10 || b[p+8]&2 != 0)) {
				return Raster{}, ErrInvalid
			}
			p += 8 + int(n) + (int(n) & 1)
			if p > len(b) {
				return Raster{}, ErrInvalid
			}
		}
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return Raster{}, ErrInvalid
	}
	// APNG's acTL must precede IDAT. Reject it before decode, also malformed chunks.
	if mime == "image/png" {
		for p := 8; p+12 <= len(b); {
			n := int(uint32(b[p])<<24 | uint32(b[p+1])<<16 | uint32(b[p+2])<<8 | uint32(b[p+3]))
			if n > len(b)-p-12 {
				return Raster{}, ErrInvalid
			}
			kind := string(b[p+4 : p+8])
			if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
				return Raster{}, ErrInvalid
			}
			p += n + 12
		}
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return Raster{}, ErrInvalid
	}
	if (mime == "image/png" && format != "png") || (mime == "image/jpeg" && format != "jpeg") || (mime == "image/webp" && format != "webp") {
		return Raster{}, ErrInvalid
	}
	if c.Width < 1 || c.Height < 1 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > 20_000_000 {
		return Raster{}, ErrLimit
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return Raster{}, ErrInvalid
	}
	out := Raster{MIME: "image/png", Width: c.Width, Height: c.Height, Variants: map[int][]byte{}}
	for _, size := range []int{256, 512, 1024} {
		w, h := c.Width, c.Height
		if w > size || h > size {
			if w >= h {
				h = max(1, h*size/w)
				w = size
			} else {
				w = max(1, w*size/h)
				h = size
			}
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			if err := ctx.Err(); err != nil {
				return Raster{}, err
			}
			for x := 0; x < w; x++ {
				dst.SetNRGBA(x, y, color.NRGBAModel.Convert(im.At(x*c.Width/w, y*c.Height/h)).(color.NRGBA))
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return Raster{}, err
		}
		out.Variants[size] = buf.Bytes()
	}
	return out, nil
}

// ImportRemote intentionally performs zero network calls until isolated SSRF-safe
// fetching, per-redirect DNS pinning and scanner admission are integrated.
func ImportRemote(context.Context, string) ([]byte, error) { return nil, ErrUnavailable }
