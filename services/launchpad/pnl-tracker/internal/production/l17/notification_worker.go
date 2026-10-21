package l17

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"strings"
	"sync"
)

type NotificationWorker struct{ Relay delivery.Relay }

func (w NotificationWorker) RunOnce(ctx context.Context) error { return w.Relay.RunOnce(ctx) }

type RevisionTracker struct {
	mu        sync.Mutex
	revisions map[string]string
	snapshots map[string]string
}

func (t *RevisionTracker) Accept(p auth.IdentityPrincipal, n Notice) (bool, error) {
	if e := RequireOwner(n.Wallet, p.Wallet); e != nil {
		return false, e
	}
	if p.SessionID == "" || p.AccountID == "" || len(p.SessionID) > 128 || len(p.AccountID) > 128 || strings.ContainsRune(p.SessionID+p.AccountID, 0) || n.Algorithm != Algorithm || !validUUID(n.SnapshotID) {
		return false, ErrInvalid
	}
	revision, e := uint256(n.Revision)
	if e != nil || revision.Sign() == 0 {
		return false, ErrInvalid
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.revisions == nil {
		t.revisions = map[string]string{}
		t.snapshots = map[string]string{}
	}
	wallet, _ := Wallet(n.Wallet)
	key := p.SessionID + "\x00" + p.AccountID + "\x00" + wallet
	if old, ok := t.revisions[key]; ok {
		previous, _ := uint256(old)
		if revision.Cmp(previous) == 0 && t.snapshots[key] != n.SnapshotID {
			return false, ErrConflict
		}
		if revision.Cmp(previous) <= 0 {
			return false, nil
		}
	} else if len(t.revisions) >= 1024 {
		return false, ErrBusy
	}
	t.revisions[key] = n.Revision
	t.snapshots[key] = n.SnapshotID
	return true, nil
}
func (t *RevisionTracker) ForgetSession(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.revisions {
		if len(key) > len(id) && key[:len(id)+1] == id+"\x00" {
			delete(t.revisions, key)
			delete(t.snapshots, key)
		}
	}
}
