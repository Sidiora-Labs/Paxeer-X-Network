package rest

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type AdmissionClass uint8

const (
	AdmissionCached AdmissionClass = iota
	AdmissionAggregate
	AdmissionHistory
	AdmissionRecovery
	AdmissionHealth
)

type AdmissionStats struct {
	Used, PublicUsed, Waiting     int
	Admitted, Rejected, Cancelled uint64
}
type Admission struct {
	mu                                                              sync.Mutex
	capacity, publicCapacity, used, publicUsed, waiting, maxWaiting int
	wait                                                            time.Duration
	changed                                                         chan struct{}
	admitted, rejected, cancelled                                   uint64
}

func NewAdmission(capacity, maxWaiting int, wait time.Duration) (*Admission, error) {
	if capacity < 10 || capacity > 10000 || maxWaiting < 1 || maxWaiting > 10000 || wait <= 0 || wait > 50*time.Millisecond {
		return nil, errors.New("invalid admission allocation")
	}
	return &Admission{capacity: capacity, publicCapacity: capacity * 4 / 5, maxWaiting: maxWaiting, wait: wait, changed: make(chan struct{})}, nil
}
func admissionCost(class AdmissionClass) (int, bool, error) {
	switch class {
	case AdmissionCached:
		return 1, false, nil
	case AdmissionAggregate:
		return 4, false, nil
	case AdmissionHistory:
		return 8, false, nil
	case AdmissionRecovery:
		return 1, true, nil
	case AdmissionHealth:
		return 1, true, nil
	default:
		return 0, false, errors.New("invalid admission class")
	}
}
func (a *Admission) Stats() AdmissionStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AdmissionStats{a.used, a.publicUsed, a.waiting, a.admitted, a.rejected, a.cancelled}
}
func (a *Admission) Acquire(ctx context.Context, class AdmissionClass) (func(), error) {
	if a == nil || ctx == nil {
		return nil, errors.New("admission unavailable")
	}
	cost, reserved, err := admissionCost(class)
	if err != nil {
		return nil, err
	}
	wait, cancel := context.WithTimeout(ctx, a.wait)
	defer cancel()
	registered := false
	defer func() {
		if registered {
			a.mu.Lock()
			a.waiting--
			a.mu.Unlock()
		}
	}()
	for {
		if err = wait.Err(); err != nil {
			a.mu.Lock()
			if ctx.Err() != nil {
				a.cancelled++
			} else {
				a.rejected++
			}
			a.mu.Unlock()
			return nil, err
		}
		a.mu.Lock()
		if err = ctx.Err(); err != nil {
			a.cancelled++
			a.mu.Unlock()
			return nil, err
		}
		if a.used+cost <= a.capacity && (reserved || a.publicUsed+cost <= a.publicCapacity) {
			a.used += cost
			if !reserved {
				a.publicUsed += cost
			}
			a.admitted++
			a.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					a.mu.Lock()
					a.used -= cost
					if !reserved {
						a.publicUsed -= cost
					}
					close(a.changed)
					a.changed = make(chan struct{})
					a.mu.Unlock()
				})
			}, nil
		}
		if !registered {
			if a.waiting >= a.maxWaiting {
				a.rejected++
				a.mu.Unlock()
				return nil, ErrCacheFillSaturated
			}
			a.waiting++
			registered = true
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-wait.Done():
		case <-changed:
		}
	}
}
func (a *Admission) Middleware(class AdmissionClass) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			release, err := a.Acquire(r.Context(), class)
			if err != nil {
				WriteReadError(w, r, ErrCacheFillSaturated)
				return
			}
			defer release()
			next.ServeHTTP(w, r)
		})
	}
}
