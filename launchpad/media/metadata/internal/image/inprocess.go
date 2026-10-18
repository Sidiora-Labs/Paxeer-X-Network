package image

import (
	"context"
	"errors"
	"time"
)

// InProcessWorker decodes and re-encodes rasters inside the calling process.
// It exists for development deployments (Railway) where ProcessWorker's
// cgroup v2 delegation is unavailable. It applies the same decoder
// (ReencodeSupported), the same 6 MiB input, 8192x8192 and 20 MP limits, the
// same five-second wall deadline and the same output validation as
// ProcessWorker; it lacks only the separate address space and the 256 MiB
// memory / 32 pid cgroup ceilings. Production wiring never uses it.
type InProcessWorker struct{}

func (InProcessWorker) Ready() error { return nil }

func (InProcessWorker) Decode(ctx context.Context, body []byte) (Raster, error) {
	if len(body) > MaxBytes {
		return Raster{}, ErrLimit
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, e := ReencodeSupported(bounded, body)
	if e != nil {
		if ctx.Err() != nil {
			return Raster{}, ctx.Err()
		}
		if errors.Is(e, context.DeadlineExceeded) {
			return Raster{}, ErrLimit
		}
		return Raster{}, e
	}
	if result.MIME != "image/png" || result.Width < 1 || result.Height < 1 || result.Width > 8192 || result.Height > 8192 || int64(result.Width)*int64(result.Height) > 20000000 || len(result.Variants) != 3 {
		return Raster{}, ErrInvalid
	}
	for _, n := range []int{256, 512, 1024} {
		b := result.Variants[n]
		if len(b) == 0 || len(b) > MaxBytes {
			return Raster{}, ErrInvalid
		}
	}
	return result, nil
}
