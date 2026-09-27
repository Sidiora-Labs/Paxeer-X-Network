package composite

import (
	"fmt"
	"sort"

	ics23 "github.com/confio/ics23/go"
	"github.com/sidiora-labs/paxeer-network/storage/common/keys"
	"github.com/sidiora-labs/paxeer-network/storage/proto"
	"github.com/sidiora-labs/paxeer-network/storage/state_db/sc/types"
	db "github.com/tendermint/tm-db"
)

var _ types.CommitKVStore = (*pendingStore)(nil)

// pendingStore is the view the composite store hands out for a module store
// the caller mounted but the state-commitment database carries no tree for.
//
// It exists so that a binary whose mount list has grown can run on a state
// that predates the growth. The store reads as empty and commits nothing:
// the database holds no tree for the name, so the name appears in no commit
// info, contributes no store hash to the root, and moves neither the working
// hash nor the app hash. Writes aimed at it are held for the block that made
// them and discarded when that block commits, which keeps every node that
// mounts the store byte for byte identical to a node that does not and leaves
// the persisted state exactly as the older binary left it.
//
// The state is temporary by construction: a load whose store upgrades add the
// name creates the real tree at that version, refreshPendingStores drops the
// name, and from then on the store is an ordinary child store whose writes
// persist and whose hash joins the root.
type pendingStore struct {
	owner *CompositeCommitStore
	name  string
}

// newPendingStore binds a pending view to name. The view is built per call,
// exactly as the ordinary child-store view is; the held writes live on the
// composite store rather than on the view, so they survive a caller that drops
// the view mid-block and are discarded together at Commit.
func (cs *CompositeCommitStore) newPendingStore(name string) *pendingStore {
	return &pendingStore{owner: cs, name: name}
}

// Get finds nothing: the database carries no tree for this store, so it holds
// no committed value at any key.
func (s *pendingStore) Get(_ []byte) []byte { return nil }

// Has finds nothing, for the same reason as Get.
func (s *pendingStore) Has(_ []byte) bool { return false }

// Set holds a write for the current block instead of routing it to a backend.
// Deprecated along with the rest of the direct write path on CommitKVStore;
// production writes arrive through CompositeCommitStore.ApplyChangeSets.
func (s *pendingStore) Set(key, value []byte) {
	s.owner.holdPendingWrite(s.name, &proto.KVPair{Key: key, Value: value})
}

// Remove holds a deletion for the current block instead of routing it to a
// backend. Deprecated along with Set.
func (s *pendingStore) Remove(key []byte) {
	s.owner.holdPendingWrite(s.name, &proto.KVPair{Key: key, Delete: true})
}

// Version reports the version of the state the composite store has loaded,
// which is what every other child store reports through this interface.
func (s *pendingStore) Version() int64 { return s.owner.Version() }

// Iterator walks an empty range: the composite store stitches one iterator
// per backend holding the name, and no backend holds this one.
func (s *pendingStore) Iterator(start, end []byte, ascending bool) db.Iterator {
	iter, err := s.owner.iterate(s.name, start, end, ascending)
	if err != nil {
		panic(fmt.Errorf("pendingStore.Iterator(store=%q): %w", s.name, err))
	}
	return iter
}

// RootHash fails closed. A pending store is in no tree, so it has no
// authenticated root, and a zero digest would be indistinguishable from the
// root of an empty tree that the commit info does carry. The router-backed
// view every other child store is served through fails closed here too.
func (s *pendingStore) RootHash() []byte {
	panic(fmt.Errorf(
		"pendingStore.RootHash(store=%q): the state-commitment database carries no tree for this store",
		s.name,
	))
}

// GetProof fails closed, for the same reason as RootHash: there is no tree to
// prove membership or absence against, and the memiavl proof builder the
// ordinary view reaches reports the missing store as an error.
func (s *pendingStore) GetProof(_ []byte) *ics23.CommitmentProof {
	panic(fmt.Errorf(
		"pendingStore.GetProof(store=%q): the state-commitment database carries no tree for this store",
		s.name,
	))
}

// Close is illegal during the standard CommitKVStore lifecycle: the composite
// store owns the backends this view reads through and must outlive it.
func (s *pendingStore) Close() error {
	return fmt.Errorf("pendingStore.Close(store=%q): illegal during standard lifecycle", s.name)
}

// refreshPendingStores recomputes which mounted store names the
// state-commitment database carries no tree for. It runs after every load and
// after every applied set of tree upgrades, the only two points at which the
// set of trees changes shape.
//
// A name is pending when the caller mounted it through Initialize, it is a
// member of keys.MemIAVLStoreKeys, and memiavl holds no tree for it. A name
// the caller never mounted is a misconfiguration rather than a store waiting
// for its upgrade, and a name outside the canonical list keeps the behaviour
// it has always had, which is to panic in GetChildStoreByName.
//
// The memiavl tree set is only the whole authority on which stores exist while
// memiavl is the only backend. Once flatkv participates a store may legitimately
// live there with no memiavl tree, so no name is pending in those modes and
// every one of them keeps today's behaviour.
func (cs *CompositeCommitStore) refreshPendingStores() {
	cs.pendingMtx.Lock()
	defer cs.pendingMtx.Unlock()

	cs.pendingWrites = nil

	if cs.memIAVL == nil || !cs.memIAVL.IsLoaded() || cs.flatKV != nil {
		cs.pending = nil
		return
	}

	var pending map[string]struct{}
	for _, name := range cs.initialStores {
		if !keys.IsMemIAVLStoreKey(name) {
			continue
		}
		if cs.memIAVL.GetChildStoreByName(name) != nil {
			continue
		}
		if pending == nil {
			pending = make(map[string]struct{}, len(cs.initialStores))
		}
		pending[name] = struct{}{}
	}
	cs.pending = pending

	if len(pending) > 0 {
		names := make([]string, 0, len(pending))
		for name := range pending {
			names = append(names, name)
		}
		sort.Strings(names)
		logger.Info(
			"mounted stores have no tree in the state-commitment database; "+
				"serving them empty and discarding their writes until a store upgrade adds them",
			"stores", names, "version", cs.memIAVL.Version(),
		)
	}
}

// IsPendingStore reports whether name is a mounted store the
// state-commitment database carries no tree for yet. Writes aimed at such a
// store hold no versioned state and must not be recorded anywhere that
// outlives the block.
func (cs *CompositeCommitStore) IsPendingStore(name string) bool {
	cs.pendingMtx.RLock()
	defer cs.pendingMtx.RUnlock()
	_, ok := cs.pending[name]
	return ok
}

// PendingStores returns, in name order, the mounted stores the
// state-commitment database carries no tree for yet.
func (cs *CompositeCommitStore) PendingStores() []string {
	cs.pendingMtx.RLock()
	defer cs.pendingMtx.RUnlock()
	if len(cs.pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(cs.pending))
	for name := range cs.pending {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// holdPendingWrite records one write aimed at a pending store for the
// current block. Calling it for a store that is not pending is a programming
// error; the callers reach it only through a pending view.
func (cs *CompositeCommitStore) holdPendingWrite(name string, pair *proto.KVPair) {
	cs.pendingMtx.Lock()
	defer cs.pendingMtx.Unlock()
	if _, ok := cs.pending[name]; !ok {
		return
	}
	if cs.pendingWrites == nil {
		cs.pendingWrites = make(map[string][]*proto.KVPair, len(cs.pending))
	}
	cs.pendingWrites[name] = append(cs.pendingWrites[name], pair)
}

// holdPendingChangeSets moves every change set aimed at a pending store out
// of the batch and into the block's held writes, returning the change sets
// that are still bound for a backend. With nothing pending it returns the
// batch it was given, so a database that carries every mounted store follows
// exactly the path it followed before pending stores existed.
func (cs *CompositeCommitStore) holdPendingChangeSets(
	changesets []*proto.NamedChangeSet,
) []*proto.NamedChangeSet {
	cs.pendingMtx.Lock()
	defer cs.pendingMtx.Unlock()

	if len(cs.pending) == 0 {
		return changesets
	}
	held := 0
	for _, ncs := range changesets {
		if _, ok := cs.pending[ncs.Name]; ok {
			held++
		}
	}
	if held == 0 {
		return changesets
	}
	if cs.pendingWrites == nil {
		cs.pendingWrites = make(map[string][]*proto.KVPair, held)
	}
	routed := make([]*proto.NamedChangeSet, 0, len(changesets)-held)
	for _, ncs := range changesets {
		if _, ok := cs.pending[ncs.Name]; !ok {
			routed = append(routed, ncs)
			continue
		}
		cs.pendingWrites[ncs.Name] = append(cs.pendingWrites[ncs.Name], ncs.Changeset.Pairs...)
	}
	return routed
}

// HeldPendingWrites returns a copy of the writes aimed at name that the
// current block is holding. It is the only way to observe them: they are
// never routed to a backend and Commit discards them.
func (cs *CompositeCommitStore) HeldPendingWrites(name string) []*proto.KVPair {
	cs.pendingMtx.RLock()
	defer cs.pendingMtx.RUnlock()
	held := cs.pendingWrites[name]
	if len(held) == 0 {
		return nil
	}
	return append([]*proto.KVPair(nil), held...)
}

// discardPendingWrites drops every write the block held for a pending store.
// Called once a commit has landed on the backends, which is the point at
// which a write that reached a real store becomes state and a write that
// reached a pending store must stop existing.
func (cs *CompositeCommitStore) discardPendingWrites() {
	cs.pendingMtx.Lock()
	defer cs.pendingMtx.Unlock()
	cs.pendingWrites = nil
}
