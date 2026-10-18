package policy

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type EdgeLease struct {
	Origin        string    `json:"origin"`
	PolicyVersion string    `json:"policyVersion"`
	ID            string    `json:"id"`
	Issued        time.Time `json:"issued"`
	Expires       time.Time `json:"expires"`
	PerSecond     int       `json:"perSecond"`
}
type EdgeAdmission struct {
	mu     sync.Mutex
	lease  EdgeLease
	second int64
	used   int
}

func NewEdgeAdmission(lease EdgeLease, signature []byte, key ed25519.PublicKey, origin string, now time.Time) (*EdgeAdmission, error) {
	wire, err := json.Marshal(lease)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, wire, signature) || lease.Origin == "" || lease.Origin != origin || lease.PolicyVersion == "" || lease.ID == "" || lease.PerSecond < 1 || lease.PerSecond > 100 || lease.Issued.After(now) || !lease.Expires.After(now) || lease.Expires.Sub(lease.Issued) > time.Minute {
		return nil, ErrDenied
	}
	return &EdgeAdmission{lease: lease}, nil
}
func (e *EdgeAdmission) Take(now time.Time) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if now.Before(e.lease.Issued) || !now.Before(e.lease.Expires) {
		return false
	}
	second := now.Unix()
	if second < e.second {
		return false
	}
	if second != e.second {
		e.second = second
		e.used = 0
	}
	if e.used >= e.lease.PerSecond {
		return false
	}
	e.used++
	return true
}

type AdmissionOptions struct {
	Redis            *redis.Client
	Namespace, Class string
	Cost             int64
	Verifier         Verifier
	CachedPublicRead bool
	Edge             *EdgeAdmission
}

func Admission(o AdmissionOptions, next http.Handler) (http.Handler, error) {
	return admissionCaptured(o, next, nil)
}
func MeasuredAdmission(o AdmissionOptions, next http.Handler, capture *MiddlewareCapture) (http.Handler, error) {
	if capture == nil {
		return nil, ErrDenied
	}
	return admissionCaptured(o, next, capture)
}
func admissionCaptured(o AdmissionOptions, next http.Handler, capture *MiddlewareCapture) (http.Handler, error) {
	if o.Redis == nil || o.Namespace == "" || len(o.Namespace) > 100 || o.Cost < 1 || next == nil {
		return nil, ErrDenied
	}
	budget, exists := frozenBudget(o.Class)
	if !exists || o.Cost > budget.Burst || o.Class != "public-read" && o.Verifier == nil || o.CachedPublicRead && o.Class != "public-read" {
		return nil, ErrDenied
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := ""
		if o.Class == "public-read" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil {
				http.Error(w, "invalid principal", 400)
				return
			}
			principal = ip.String()
		} else {
			started := time.Now()
			id, err := o.Verifier.Verify(r.Context(), r)
			identityAllowed := err == nil && id.Actor != "" && id.Expires.After(time.Now()) && !id.Issued.After(time.Now())
			if capture != nil && capture.Record(r, "authorization", started, identityAllowed, err != nil && !errors.Is(err, ErrDenied)) != nil {
				http.Error(w, "capture_unavailable", 503)
				return
			}
			if err != nil || id.Actor == "" || !id.Expires.After(time.Now()) || id.Issued.After(time.Now()) {
				http.Error(w, "unauthorized", 401)
				return
			}
			principal = id.Actor
		}
		started := time.Now()
		allowed, retry, err := Take(r.Context(), o.Redis, o.Namespace, o.Class, principal, o.Cost)
		fallbackAllowed := err != nil && o.Class == "public-read" && o.CachedPublicRead && o.Edge.Take(time.Now())
		if capture != nil && capture.Record(r, "limiter", started, allowed || fallbackAllowed, err != nil) != nil {
			http.Error(w, "capture_unavailable", 503)
			return
		}
		if err != nil {
			if fallbackAllowed {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "request_budget_unavailable", 503)
			return
		}
		if !allowed {
			seconds := int(math.Ceil(retry.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "request_budget_exhausted", 429)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
