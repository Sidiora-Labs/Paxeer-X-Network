package upload

import (
	"bytes"
	"context"
	redis "github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDurableAdmission(t *testing.T) {
	ctx := context.Background()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	ns := "kd-fec98588ef6fe4334797a5de-a" + time.Now().Format("150405000000000")
	a, e := NewAdmission(db, ns, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ks, _ := db.Keys(ctx, ns+":*").Result()
		if len(ks) > 0 {
			db.Del(ctx, ks...)
		}
	}()
	wallet := "0x1234567890abcdef1234567890abcdef12345678"
	r1, e := a.Acquire(ctx, wallet)
	if e != nil {
		t.Fatal(e)
	}
	r2, e := a.Acquire(ctx, wallet)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Acquire(ctx, wallet); e == nil {
		t.Fatal("concurrent limit")
	}
	r1()
	r1()
	r2()
	for i := 0; i < 8; i++ {
		r, e := a.Acquire(ctx, wallet)
		if e != nil {
			t.Fatal(i, e)
		}
		r()
	}
	if _, e = a.Acquire(ctx, wallet); e == nil {
		t.Fatal("rate bypass")
	}
}

type countingBody struct{ reads int }

func (b *countingBody) Read(p []byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *countingBody) Close() error               { return nil }
func TestConfiguredAdmissionRejectsUnauthenticatedBeforeBody(t *testing.T) {
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	a, e := NewAdmission(db, "kd-fec98588ef6fe4334797a5de", 2)
	if e != nil {
		t.Fatal(e)
	}
	h := New(Deps{Admission: a, MetadataBaseURL: "http://127.0.0.1:1", MaxBytes: 6 << 20})
	b := &countingBody{}
	r := httptest.NewRequest("POST", "/upload/token/0x1234567890abcdef1234567890abcdef12345678", b)
	w := httptest.NewRecorder()
	h.upload(w, r)
	if w.Code != 401 || b.reads != 0 {
		t.Fatal(w.Code, b.reads)
	}
}

func TestUploadRedirectNeverFollows(t *testing.T) {
	var calls atomic.Int32
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer dest.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 302) }))
	defer up.Close()
	h := New(Deps{MetadataBaseURL: up.URL, MaxBytes: 6 << 20})
	r := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.forward(w, r, testAddr, bytes.NewBufferString("owned synthetic multipart"), "multipart/form-data")
	if w.Code != 502 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
}
