package policy

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"sync"
	"time"
)

type RotatingKeys struct {
	mu        sync.RWMutex
	rings     map[string]*KeyRing
	refreshed time.Time
}

func (k *RotatingKeys) Replace(values []secrets.VersionedValue) error {
	if len(values) > 128 {
		return ErrDenied
	}
	grouped := map[string][]Key{}
	for _, value := range values {
		v := value.Record
		if v.ID == "" || v.Audience == "" {
			return ErrDenied
		}
		grouped[v.ID] = append(grouped[v.ID], Key{ID: v.ID, Version: v.Version, Audience: v.Audience, Activate: v.Activate, Retire: v.Retire, Value: value.Value, Revoked: v.Revoked})
	}
	rings := map[string]*KeyRing{}
	for id, keys := range grouped {
		ring := &KeyRing{}
		if err := ring.Replace(keys); err != nil {
			return err
		}
		rings[id] = ring
	}
	k.mu.Lock()
	k.rings = rings
	k.refreshed = time.Now()
	k.mu.Unlock()
	return nil
}
func (k *RotatingKeys) Sign(id, audience string, body []byte, now time.Time) (string, string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.refreshed.IsZero() || now.Sub(k.refreshed) > time.Minute || now.Before(k.refreshed) || k.rings[id] == nil {
		return "", "", ErrDenied
	}
	return k.rings[id].Sign(audience, body, now)
}
func (k *RotatingKeys) Verify(id, audience, version, signature string, body []byte, now time.Time) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.refreshed.IsZero() || now.Sub(k.refreshed) > time.Minute || now.Before(k.refreshed) || k.rings[id] == nil {
		return false
	}
	return k.rings[id].Verify(audience, version, signature, body, now)
}
func (k *RotatingKeys) Start(ctx context.Context, source secrets.VersionSource, interval time.Duration) (*secrets.Rotation, error) {
	return secrets.NewRotation(ctx, source, interval, k.Replace)
}
