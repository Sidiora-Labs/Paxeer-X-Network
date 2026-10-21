package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestProductionRaster(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 16, 12)))
	out, e := Reencode(context.Background(), b.Bytes())
	if e != nil || len(out.Variants) != 3 {
		t.Fatalf("%+v %v", out, e)
	}
	for _, v := range out.Variants {
		if _, e := png.Decode(bytes.NewReader(v)); e != nil {
			t.Fatal(e)
		}
	}
}
func TestProductionBombBeforeDecode(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	p := b.Bytes()
	binary.BigEndian.PutUint32(p[16:20], 8193)
	binary.BigEndian.PutUint32(p[29:33], crc32.ChecksumIEEE(p[12:29]))
	if _, e := Reencode(context.Background(), p); e != ErrLimit {
		t.Fatal(e)
	}
}
func TestProductionRefusesActiveAndOversized(t *testing.T) {
	for _, s := range []string{"<svg/>", "<html>"} {
		if _, e := Reencode(context.Background(), []byte(s)); e != ErrInvalid {
			t.Fatal(e)
		}
	}
	if _, e := ReadBounded(strings.NewReader(strings.Repeat("x", MaxBytes+1))); e != ErrLimit {
		t.Fatal(e)
	}
	if _, e := ImportRemote(context.Background(), "http://169.254.169.254"); e != ErrUnavailable {
		t.Fatal(e)
	}
}
func TestProductionCancelledDecode(t *testing.T) {
	ctx, c := context.WithCancel(context.Background())
	c()
	if _, e := Reencode(ctx, nil); e != context.Canceled {
		t.Fatal(e)
	}
}
