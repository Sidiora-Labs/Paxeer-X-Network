// Package ogrender renders shareable PnL card images (OG cards) to PNG using
// github.com/fogleman/gg. Inter *.ttf faces are loaded at runtime from a
// configurable font directory; when a face is missing the renderer falls back to
// the built-in basic font so rendering never fails (and tests need no bundled
// binaries). The renderer is purely presentational — all money math and number
// formatting happen upstream (invariant i1) and arrive here as display strings.
package ogrender

import (
	"bytes"
	"fmt"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/fogleman/gg"
	"golang.org/x/image/font/basicfont"
)

// Card dimensions: the standard Open Graph image size.
const (
	width  = 1200
	height = 630
)

// Palette (minimal, dark, single accent — no gradients/glow/borders).
const (
	colorBG     = "#0B0B0F"
	colorAccent = "#004CED"
	colorText   = "#FFFFFF"
	colorMuted  = "#A1A1AA"
	colorGain   = "#16A34A"
	colorLoss   = "#DC2626"
)

// Font face files (loaded from the configured dir when present).
const (
	fontBold    = "Inter-Bold.ttf"
	fontRegular = "Inter-Regular.ttf"
)

// Renderer draws PnL cards. It is safe for concurrent use (each Render builds its
// own drawing context).
type Renderer struct {
	fontDir string
	strict  bool
}

// New builds a Renderer that loads fonts from fontDir (may be empty, which forces
// the basic-font fallback).
func New(fontDir string) *Renderer { return &Renderer{fontDir: fontDir} }

func NewStrict(fontDir string) (*Renderer, error) {
	r := &Renderer{fontDir: fontDir, strict: true}
	if err := r.validateFonts(); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Renderer) validateFonts() error {
	if r.fontDir == "" {
		return fmt.Errorf("ogrender: required font directory missing")
	}
	dc := gg.NewContext(1, 1)
	for _, name := range []string{fontBold, fontRegular} {
		if err := dc.LoadFontFace(filepath.Join(r.fontDir, name), 36); err != nil {
			return fmt.Errorf("ogrender: required font %s: %w", name, err)
		}
	}
	return nil
}

// Input is the flat, already-formatted content drawn onto a card.
type Input struct {
	Title       string // headline, e.g. token name / "$SYMBOL"
	Multiple    string // e.g. "2.5x"
	Pnl         string // e.g. "+$3.50" or "-$6.00"
	PnlPositive bool
	Holdings    string // e.g. "1.50 SYMBOL held"
	Footer      string // e.g. "sidiora.fun/pnl/abc123"
}

// Render draws in onto a 1200x630 PNG and returns the encoded bytes.
func (r *Renderer) Render(in Input) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("ogrender: renderer missing")
	}
	for _, text := range []string{in.Title, in.Multiple, in.Pnl, in.Holdings, in.Footer} {
		if !utf8.ValidString(text) || len(text) > 4096 {
			return nil, fmt.Errorf("ogrender: invalid label")
		}
		for _, character := range text {
			if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
				return nil, fmt.Errorf("ogrender: unsupported label control")
			}
		}
	}
	if r.strict {
		if err := r.validateFonts(); err != nil {
			return nil, err
		}
	}
	dc := gg.NewContext(width, height)

	// Background + top accent bar.
	dc.SetHexColor(colorBG)
	dc.Clear()
	dc.SetHexColor(colorAccent)
	dc.DrawRectangle(0, 0, float64(width), 14)
	dc.Fill()

	// Headline.
	if err := r.setFace(dc, fontBold, 64); err != nil {
		return nil, err
	}
	dc.SetHexColor(colorText)
	dc.DrawStringAnchored(truncate(in.Title, 28), 80, 150, 0, 0.5)

	// Multiple (the hero number).
	if err := r.fitFace(dc, fontBold, 130, in.Multiple); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored(in.Multiple, 80, 330, 0, 0.5)

	// Signed PnL, coloured by sign.
	if err := r.fitFace(dc, fontBold, 76, in.Pnl); err != nil {
		return nil, err
	}
	if in.PnlPositive {
		dc.SetHexColor(colorGain)
	} else {
		dc.SetHexColor(colorLoss)
	}
	dc.DrawStringAnchored(in.Pnl, 80, 450, 0, 0.5)

	// Holdings + footer.
	dc.SetHexColor(colorMuted)
	if err := r.setFace(dc, fontRegular, 36); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored(truncate(in.Holdings, 48), 80, 530, 0, 0.5)
	if r.strict {
		if err := r.fitFace(dc, fontRegular, 36, in.Footer); err != nil {
			return nil, err
		}
		dc.DrawStringAnchored(in.Footer, 80, 588, 0, 0.5)
	} else {
		dc.DrawStringAnchored(truncate(in.Footer, 48), 80, 588, 0, 0.5)
	}

	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, fmt.Errorf("ogrender: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// setFace loads the requested Inter face at the given size, falling back to the
// built-in basic font when the directory is unset or the file can't be loaded.
func (r *Renderer) setFace(dc *gg.Context, file string, points float64) error {
	if r.fontDir != "" {
		if err := dc.LoadFontFace(filepath.Join(r.fontDir, file), points); err == nil {
			return nil
		} else if r.strict {
			return fmt.Errorf("ogrender: required font %s: %w", file, err)
		}
	}
	dc.SetFontFace(basicfont.Face7x13)
	return nil
}

// truncate bounds a label to n runes, appending an ellipsis when clipped.
func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "\u2026"
}

func (r *Renderer) fitFace(dc *gg.Context, file string, size float64, text string) error {
	for points := size; points >= 20; points -= 2 {
		if err := r.setFace(dc, file, points); err != nil {
			return err
		}
		w, _ := dc.MeasureString(text)
		if w <= 1040 {
			return nil
		}
	}
	return fmt.Errorf("ogrender: amount or label exceeds image width")
}
