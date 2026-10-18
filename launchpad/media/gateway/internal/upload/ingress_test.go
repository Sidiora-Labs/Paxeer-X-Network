package upload

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfiguredAdmissionIngressDeadlineClosesSlowConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		body := &ingressReader{ReadCloser: r.Body, controller: http.NewResponseController(w), window: now, deadline: now.Add(30 * time.Second)}
		_, err := io.Copy(io.Discard, body)
		if err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if _, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: fixture\r\nContent-Length: 100000\r\nConnection: close\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 408 || time.Since(began) > 7*time.Second {
		t.Fatal(response.StatusCode, time.Since(began))
	}
}
