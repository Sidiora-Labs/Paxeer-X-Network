package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ScanVerdict string

const (
	ScanClean       ScanVerdict = "CLEAN"
	ScanInfected    ScanVerdict = "INFECTED"
	ScanUnavailable ScanVerdict = "UNAVAILABLE"
	ScanError       ScanVerdict = "ERROR"
)

type ScanResult struct {
	ContentHash       string
	Size              int64
	Clean             bool
	Reason            string
	Verdict           ScanVerdict
	Engine            string
	SignatureRevision string
}
type ScanOptions struct {
	Host              string
	Port              int
	Timeout           time.Duration
	Engine            string
	SignatureRevision string
}

// ScanBuffer never interprets an absent, interrupted or malformed scan as clean.
func ScanBuffer(ctx context.Context, data []byte, opts ScanOptions) ScanResult {
	fail := func(v ScanVerdict, r string) ScanResult {
		return ScanResult{Verdict: v, Reason: r, Engine: opts.Engine, SignatureRevision: opts.SignatureRevision}
	}
	if len(data) > 6<<20 {
		return fail(ScanError, "scan-size-limit")
	}
	data = bytes.Clone(data)
	host := opts.Host
	if host == "" {
		host = os.Getenv("CLAMAV_HOST")
	}
	if host == "" {
		return fail(ScanUnavailable, "clamav-not-configured")
	}
	port := opts.Port
	if port == 0 {
		configured := os.Getenv("CLAMAV_PORT")
		if configured == "" {
			port = 3310
		} else {
			var e error
			port, e = strconv.Atoi(configured)
			if e != nil {
				return fail(ScanError, "clamav-invalid-port")
			}
		}
	}
	if port < 1 || port > 65535 {
		return fail(ScanError, "clamav-invalid-port")
	}
	timeout := opts.Timeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fail(ScanUnavailable, "clamav-unreachable")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return fail(ScanUnavailable, "clamav-deadline")
	}
	write := func(b []byte) bool {
		for len(b) > 0 {
			n, e := conn.Write(b)
			if e != nil || n == 0 {
				return false
			}
			b = b[n:]
		}
		return true
	}
	if !write([]byte("zINSTREAM\x00")) {
		return fail(ScanUnavailable, "clamav-interrupted")
	}
	for remaining := data; len(remaining) > 0; {
		n := len(remaining)
		if n > 65536 {
			n = 65536
		}
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(n))
		if !write(hdr[:]) || !write(remaining[:n]) {
			return fail(ScanUnavailable, "clamav-interrupted")
		}
		remaining = remaining[n:]
	}
	if !write(make([]byte, 4)) {
		return fail(ScanUnavailable, "clamav-interrupted")
	}
	reply, err := bufio.NewReader(io.LimitReader(conn, 4097)).ReadString(0)
	if err != nil || len(reply) > 4096 {
		return fail(ScanUnavailable, "clamav-incomplete-verdict")
	}
	reply = strings.TrimSuffix(reply, "\x00")
	if reply == "stream: OK" {
		r := fail(ScanClean, "")
		r.Clean = true
		h := sha256.Sum256(data)
		r.ContentHash = hex.EncodeToString(h[:])
		r.Size = int64(len(data))
		return r
	}
	if strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND") && len(reply) > len("stream:  FOUND") {
		return fail(ScanInfected, "malware-detected")
	}
	return fail(ScanError, "clamav-invalid-verdict")
}

func ScannerVersion(ctx context.Context, opts ScanOptions) (ScanOptions, error) {
	host := opts.Host
	if host == "" {
		host = os.Getenv("CLAMAV_HOST")
	}
	if host == "" {
		return opts, fmt.Errorf("scanner unavailable")
	}
	port := opts.Port
	if port == 0 {
		configured := os.Getenv("CLAMAV_PORT")
		if configured == "" {
			port = 3310
		} else {
			var e error
			port, e = strconv.Atoi(configured)
			if e != nil {
				return opts, fmt.Errorf("scanner unavailable")
			}
		}
	}
	if port < 1 || port > 65535 {
		return opts, fmt.Errorf("scanner unavailable")
	}
	timeout := opts.Timeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, e := (&net.Dialer{}).DialContext(bounded, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if e != nil {
		return opts, fmt.Errorf("scanner unavailable")
	}
	defer conn.Close()
	stop := context.AfterFunc(bounded, func() { conn.Close() })
	defer stop()
	deadline, _ := bounded.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return opts, fmt.Errorf("scanner unavailable")
	}
	if _, e = io.Copy(conn, strings.NewReader("zVERSION\x00")); e != nil {
		return opts, fmt.Errorf("scanner unavailable")
	}
	reply, e := bufio.NewReader(io.LimitReader(conn, 1025)).ReadString(0)
	if e != nil || len(reply) > 1024 {
		return opts, fmt.Errorf("scanner unavailable")
	}
	parts := strings.Split(strings.TrimSuffix(reply, "\x00"), "/")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "ClamAV ") {
		return opts, fmt.Errorf("scanner unavailable")
	}
	engine := strings.TrimPrefix(parts[0], "ClamAV ")
	if !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:[-a-zA-Z0-9.]*)$`).MatchString(engine) || !regexp.MustCompile(`^[1-9][0-9]{0,15}$`).MatchString(parts[1]) || strings.TrimSpace(parts[2]) == "" {
		return opts, fmt.Errorf("scanner unavailable")
	}
	opts.Host = host
	opts.Port = port
	opts.Timeout = timeout
	opts.Engine = "ClamAV " + engine
	opts.SignatureRevision = parts[1]
	return opts, nil
}
func ScanVerifiedBuffer(ctx context.Context, data []byte, opts ScanOptions) ScanResult {
	verified, e := ScannerVersion(ctx, opts)
	if e != nil {
		return ScanResult{Verdict: ScanUnavailable, Reason: "clamav-version-unavailable"}
	}
	result := ScanBuffer(ctx, data, verified)
	if result.Verdict != ScanClean {
		return result
	}
	after, e := ScannerVersion(ctx, opts)
	if e != nil || after.Engine != verified.Engine || after.SignatureRevision != verified.SignatureRevision {
		return ScanResult{Verdict: ScanUnavailable, Reason: "clamav-version-changed"}
	}
	return result
}
