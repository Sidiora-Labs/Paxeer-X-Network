package main

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type qualificationControl struct {
	Operation string                `json:"operation"`
	Request   *qualificationRequest `json:"request,omitempty"`
	LogicalID string                `json:"logical_id,omitempty"`
}
type qualificationResponse struct {
	Record    *qualificationRecord    `json:"record,omitempty"`
	Completed *qualificationCompleted `json:"completed,omitempty"`
	Error     string                  `json:"error,omitempty"`
}

func qHandle(registry *qualificationRegistry, raw []byte) qualificationResponse {
	var in qualificationControl
	var out qualificationResponse
	fail := func(e error) qualificationResponse { return qualificationResponse{Error: e.Error()} }
	if e := qStrict(raw, &in); e != nil {
		return fail(e)
	}
	switch in.Operation {
	case "submit":
		if in.Request == nil {
			return fail(errors.New("request required"))
		}
		key, _, e := qLoadKeys(registry.root)
		if e != nil {
			return fail(e)
		}
		public, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if e != nil {
			return fail(e)
		}
		if in.Request.Recipient != base64.StdEncoding.EncodeToString(public) {
			return fail(errors.New("artifact recipient mismatch"))
		}
		rec, e := registry.submit(*in.Request)
		if e != nil {
			return fail(e)
		}
		out.Record = &rec
	case "status", "export":
		if in.Request != nil {
			return fail(errors.New("read operation cannot carry a request"))
		}
		rec, e := registry.read(in.LogicalID)
		if e != nil {
			return fail(e)
		}
		out.Record = &rec
		if in.Operation == "export" {
			if rec.State != "completed_validated" {
				return fail(errors.New("completed evidence unavailable"))
			}
			b, e := qRead(filepath.Join(registry.root, in.LogicalID+".completed.json"))
			if e != nil {
				return fail(e)
			}
			if qHash(b) != rec.Detail {
				return fail(errors.New("completed evidence mutated"))
			}
			var completed qualificationCompleted
			if e = qStrict(b, &completed); e != nil {
				return fail(e)
			}
			out.Completed = &completed
		}
	default:
		return fail(errors.New("unknown control operation"))
	}
	return out
}
func qServe(ctx context.Context, r *qualificationRegistry) error {
	if !qMounted(r.root) {
		return errors.New("qualification volume is not mounted")
	}
	if _, _, e := qLoadKeys(r.root); e != nil {
		return e
	}
	leader, e := os.OpenFile(filepath.Join(r.root, "controller.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	if e = syscall.Flock(int(leader.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		leader.Close()
		return e
	}
	socket := filepath.Join(r.root, "control.sock")
	if info, e := os.Lstat(socket); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			leader.Close()
			return errors.New("control path is not a socket")
		}
		if e = os.Remove(socket); e != nil {
			leader.Close()
			return e
		}
	}
	listener, e := net.Listen("unix", socket)
	if e != nil {
		leader.Close()
		return e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		listener.Close()
		leader.Close()
		return e
	}
	go func() { <-ctx.Done(); listener.Close() }()
	go func() {
		defer leader.Close()
		defer listener.Close()
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				b, e := io.ReadAll(io.LimitReader(conn, qualificationLimit+1))
				if e != nil {
					return
				}
				_ = json.NewEncoder(conn).Encode(qHandle(r, b))
			}()
		}
	}()
	return nil
}
func qCLI(args []string) int {
	fail := func(e error) int { fmt.Fprintln(os.Stderr, "qualification: refused:", e); return 2 }
	if len(args) == 0 {
		return fail(errors.New("operation required"))
	}
	switch args[0] {
	case "qualification-seal":
		if len(args) != 3 {
			return fail(errors.New("seal requires binding and recipient"))
		}
		b, e := io.ReadAll(io.LimitReader(os.Stdin, qualificationLimit+1))
		if e != nil || len(b) > qualificationLimit {
			return fail(errors.New("artifact size refused"))
		}
		sealed, e := qSeal(b, args[1], args[2])
		if e != nil {
			return fail(e)
		}
		if e = json.NewEncoder(os.Stdout).Encode(sealed); e != nil {
			return fail(e)
		}
	case "qualification-verify":
		if len(args) != 3 {
			return fail(errors.New("verify requires completed file and pinned trust key"))
		}
		if e := qVerifyFile(args[1], args[2], time.Now().Add(1500*time.Second)); e != nil {
			return fail(e)
		}
		fmt.Println("completed evidence validated")
	case "qualification-client":
		if len(args) != 2 {
			return fail(errors.New("client requires socket"))
		}
		b, e := io.ReadAll(io.LimitReader(os.Stdin, qualificationLimit+1))
		if e != nil || len(b) > qualificationLimit {
			return fail(errors.New("request size refused"))
		}
		conn, e := net.DialTimeout("unix", args[1], 10*time.Second)
		if e != nil {
			return fail(e)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		if _, e = conn.Write(b); e != nil {
			return fail(e)
		}
		if e = conn.(*net.UnixConn).CloseWrite(); e != nil {
			return fail(e)
		}
		response, e := io.ReadAll(io.LimitReader(conn, qualificationLimit+1))
		if e != nil {
			return fail(e)
		}
		var out qualificationResponse
		if e = qStrict(response, &out); e != nil {
			return fail(e)
		}
		if out.Error != "" {
			return fail(errors.New(out.Error))
		}
		_, e = os.Stdout.Write(response)
		if e != nil {
			return fail(e)
		}
	default:
		return fail(errors.New("unknown qualification command"))
	}
	return 0
}
