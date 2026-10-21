package metadata

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestProductionScannerProtocol(t *testing.T) {
	for _, tc := range []struct {
		reply string
		clean bool
	}{{"stream: OK\x00", true}, {"stream: NOT OK\x00", false}, {"stream: Eicar FOUND\x00", false}, {"stream: OK", false}, {"", false}} {
		t.Run(strconv.Itoa(len(tc.reply))+tc.reply, func(t *testing.T) {
			l, e := net.Listen("tcp", "127.0.0.1:23903")
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, e := l.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				io.ReadFull(c, make([]byte, 10))
				for {
					var h [4]byte
					if _, e := io.ReadFull(c, h[:]); e != nil {
						return
					}
					n := binary.BigEndian.Uint32(h[:])
					if n == 0 {
						break
					}
					if _, e := io.CopyN(io.Discard, c, int64(n)); e != nil {
						return
					}
				}
				c.Write([]byte(tc.reply))
			}()
			r := sharedhttp.ScanBuffer(context.Background(), []byte("fixture"), sharedhttp.ScanOptions{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, Timeout: time.Second})
			if r.Clean != tc.clean {
				t.Fatal(r)
			}
			<-done
		})
	}
}
func TestProductionScannerCancelled(t *testing.T) {
	ctx, c := context.WithCancel(context.Background())
	c()
	r := sharedhttp.ScanBuffer(ctx, nil, sharedhttp.ScanOptions{Host: "127.0.0.1", Port: 23903})
	if r.Clean {
		t.Fatal(r)
	}
}
func TestProductionScannerSnapshot(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:23903")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	data := []byte("original")
	read := make(chan struct{})
	release := make(chan struct{})
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		io.ReadFull(c, make([]byte, 10))
		var h [4]byte
		io.ReadFull(c, h[:])
		io.CopyN(io.Discard, c, int64(binary.BigEndian.Uint32(h[:])))
		io.ReadFull(c, h[:])
		close(read)
		<-release
		c.Write([]byte("stream: OK\x00"))
	}()
	done := make(chan sharedhttp.ScanResult)
	go func() {
		done <- sharedhttp.ScanBuffer(context.Background(), data, sharedhttp.ScanOptions{Host: "127.0.0.1", Port: 23903, Timeout: time.Second})
	}()
	<-read
	data[0] = 'X'
	close(release)
	r := <-done
	h := sha256.Sum256([]byte("original"))
	if !r.Clean || r.ContentHash != hex.EncodeToString(h[:]) {
		t.Fatal(r)
	}
}

func TestProductionActualScannerMidstreamDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	actual, e := sharedhttp.ScannerVersion(ctx, sharedhttp.ScanOptions{})
	if e != nil {
		t.Fatal("actual configured clamd is required:", e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			incoming, e := listener.Accept()
			if e != nil {
				done <- e
				return
			}
			incoming.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(incoming)
			command, e := reader.ReadString(0)
			if e != nil {
				incoming.Close()
				done <- e
				return
			}
			upstream, e := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(actual.Host, strconv.Itoa(actual.Port)))
			if e != nil {
				incoming.Close()
				done <- e
				return
			}
			upstream.SetDeadline(time.Now().Add(5 * time.Second))
			if _, e = io.WriteString(upstream, command); e != nil {
				upstream.Close()
				incoming.Close()
				done <- e
				return
			}
			if command == "zINSTREAM\x00" {
				var header [4]byte
				_, e = io.ReadFull(reader, header[:])
				if e == nil && binary.BigEndian.Uint32(header[:]) == 0 {
					e = fmt.Errorf("expected nonempty real image stream")
				}
				upstream.Close()
				incoming.Close()
				done <- e
				return
			}
			if command != "zVERSION\x00" {
				upstream.Close()
				incoming.Close()
				done <- fmt.Errorf("unexpected scanner command")
				return
			}
			reply, e := bufio.NewReader(upstream).ReadString(0)
			if e == nil {
				_, e = io.WriteString(incoming, reply)
			}
			upstream.Close()
			incoming.Close()
			if e != nil {
				done <- e
				return
			}
		}
	}()
	result := sharedhttp.ScanVerifiedBuffer(ctx, []byte("actual scanner interrupted image bytes"), sharedhttp.ScanOptions{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port})
	if result.Clean || result.Verdict != sharedhttp.ScanUnavailable || result.ContentHash != "" {
		t.Fatal("interrupted actual scanner became clean", result)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestProductionScannerPortConfigurationFailsClosed(t *testing.T) {
	t.Setenv("CLAMAV_HOST", "127.0.0.1")
	for _, port := range []string{"invalid", "0", "65536"} {
		t.Setenv("CLAMAV_PORT", port)
		if _, e := sharedhttp.ScannerVersion(context.Background(), sharedhttp.ScanOptions{}); e == nil {
			t.Fatal("invalid configured scanner port accepted", os.Getenv("CLAMAV_PORT"))
		}
		if result := sharedhttp.ScanBuffer(context.Background(), []byte("bytes"), sharedhttp.ScanOptions{}); result.Clean || result.Verdict != sharedhttp.ScanError {
			t.Fatal("scan bypassed invalid configured port", result)
		}
	}
}
