package upload

import (
	"errors"
	"io"
	"net/http"
	"time"
)

var errSlowIngress = errors.New("upload ingress too slow")

type ingressReader struct {
	io.ReadCloser
	controller *http.ResponseController
	window     time.Time
	deadline   time.Time
	received   int64
}

func (r *ingressReader) Read(p []byte) (int, error) {
	now := time.Now()
	if now.After(r.deadline) {
		return 0, errSlowIngress
	}
	next := now.Add(5 * time.Second)
	if next.After(r.deadline) {
		next = r.deadline
	}
	_ = r.controller.SetReadDeadline(next)
	n, err := r.ReadCloser.Read(p)
	if time.Now().After(r.deadline) {
		return n, errSlowIngress
	}
	r.received += int64(n)
	elapsed := time.Since(r.window)
	if elapsed >= 5*time.Second {
		if r.received < int64(elapsed.Seconds()*32768) {
			return n, errSlowIngress
		}
		r.window = time.Now()
		r.received = 0
	}
	return n, err
}
