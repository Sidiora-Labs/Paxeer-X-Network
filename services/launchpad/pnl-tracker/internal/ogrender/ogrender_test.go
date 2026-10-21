package ogrender_test

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
)

// decodePNG decodes the rendered bytes, failing the test if they are not a valid
// PNG, and returns the image config.
func decodePNG(t *testing.T, b []byte) image.Config {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode png config: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return cfg
}

func TestRenderProducesValidPNG(t *testing.T) {
	// Empty font dir forces the built-in basic-font fallback (no bundled binaries).
	r := ogrender.New("")
	out, err := r.Render(ogrender.Input{
		Title:       "Dogecoin",
		Multiple:    "2.50x",
		Pnl:         "+$3.50",
		PnlPositive: true,
		Holdings:    "1.50 DOGE held",
		Footer:      "sidiora.fun/pnl/abc1234",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("render produced no bytes")
	}
	cfg := decodePNG(t, out)
	if cfg.Width != 1200 || cfg.Height != 630 {
		t.Fatalf("dimensions = %dx%d, want 1200x630", cfg.Width, cfg.Height)
	}
}

func TestRenderHandlesLossAndLongStrings(t *testing.T) {
	r := ogrender.New("")
	// A very long title must be truncated without panicking; negative PnL renders.
	out, err := r.Render(ogrender.Input{
		Title:       "An Extremely Long Token Name That Exceeds The Card Width By A Lot",
		Multiple:    "0.10x",
		Pnl:         "-$6.00",
		PnlPositive: false,
		Holdings:    "0.00 tokens held",
		Footer:      "sidiora.fun/pnl/zzzzzzz",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	cfg := decodePNG(t, out)
	if cfg.Width != 1200 || cfg.Height != 630 {
		t.Fatalf("dimensions = %dx%d, want 1200x630", cfg.Width, cfg.Height)
	}
}

func TestRenderMissingFontDirFallsBack(t *testing.T) {
	// A non-existent font dir must still render (basic-font fallback), never error.
	r := ogrender.New("/no/such/font/dir")
	out, err := r.Render(ogrender.Input{Title: "X", Multiple: "1.00x", Pnl: "$0.00", Holdings: "0", Footer: "f"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("render produced no bytes")
	}
}

func TestRenderRejectsInvalidTextAndDirectionControls(t *testing.T) {
	for _, text := range []string{"profit\n100 USDL", "profit\u202e100 USDL", string([]byte{0xff})} {
		if _, e := ogrender.New("").Render(ogrender.Input{Title: text}); e == nil {
			t.Fatal("ambiguous metadata accepted", text)
		}
	}
}
