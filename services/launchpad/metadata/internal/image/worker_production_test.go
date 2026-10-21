package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"testing"
)

func TestProductionWorkerRequiresRealDelegation(t *testing.T) {
	if _, e := (ProcessWorker{}).Decode(context.Background(), []byte("image")); e != ErrUnavailable {
		t.Fatal(e)
	}
	if _, e := (ProcessWorker{}).Decode(context.Background(), make([]byte, MaxBytes+1)); e != ErrLimit {
		t.Fatal(e)
	}
}
func TestProductionSupportedRasterUsesActualDecoder(t *testing.T) {
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 16, 12))); e != nil {
		t.Fatal(e)
	}
	out, e := ReencodeSupported(context.Background(), b.Bytes())
	if e != nil || out.Width != 16 || out.Height != 12 || len(out.Variants) != 3 {
		t.Fatalf("%+v %v", out, e)
	}
	for _, variant := range out.Variants {
		decoded, e := png.Decode(bytes.NewReader(variant))
		if e != nil || decoded.Bounds().Dx() != 16 || decoded.Bounds().Dy() != 12 {
			t.Fatal(e)
		}
	}
	animation := append([]byte("RIFF"), make([]byte, 4)...)
	animation = append(animation, []byte("WEBPANIM")...)
	animation = append(animation, make([]byte, 4)...)
	binary.LittleEndian.PutUint32(animation[4:8], uint32(len(animation)-8))
	if _, e = ReencodeSupported(context.Background(), animation); e != ErrInvalid {
		t.Fatal(e)
	}
}

func TestProductionSupportedWebPActualDecoder(t *testing.T) {
	body, e := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	if e != nil {
		t.Fatal(e)
	}
	out, e := ReencodeSupported(context.Background(), body)
	if e != nil || out.MIME != "image/png" || out.Width < 1 || out.Height < 1 || len(out.Variants) != 3 {
		t.Fatalf("%+v %v", out, e)
	}
	for _, b := range out.Variants {
		if _, e := png.Decode(bytes.NewReader(b)); e != nil {
			t.Fatal(e)
		}
	}
}

func TestProductionActualIsolatedDimensionBomb(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	worker := ProcessWorker{Executable: executable, CgroupParent: os.Getenv("KINDLE_RASTER_CGROUP_PARENT")}
	if e = worker.Ready(); e != nil {
		t.Fatal("real delegated cgroup is required:", e)
	}
	var b bytes.Buffer
	if e = png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1))); e != nil {
		t.Fatal(e)
	}
	bomb := b.Bytes()
	binary.BigEndian.PutUint32(bomb[16:20], 8193)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	result, e := worker.Decode(context.Background(), bomb)
	if e != ErrLimit || len(result.Variants) != 0 {
		t.Fatal("dimension bomb escaped isolated limit", e, len(result.Variants))
	}
}
