package store

import (
	"bytes"
	"errors"
	"testing"
)

func TestStagedShareCommitsOrDiscardsWhole(t *testing.T) {
	dir := t.TempDir()
	key := randomBytes(t, KeySize)
	s := openStore(t, dir, key)
	f := fixtures(t)[0]
	if err := s.Put(f.rec, f.share); err != nil {
		t.Fatal(err)
	}
	next := f.rec
	next.Epoch = f.rec.Epoch + 1
	nextShare := randomBytes(t, 512)

	skipped := f.rec
	skipped.Epoch = f.rec.Epoch + 2
	if err := s.PutStaged(skipped, nextShare); !errors.Is(err, ErrStageEpoch) {
		t.Fatalf("a stage that skips an epoch: %v", err)
	}
	if err := s.PutStaged(next, nextShare); err != nil {
		t.Fatal(err)
	}
	if got, err := readShare(t, s, f.rec.KeyID); err != nil || !bytes.Equal(got, f.share) {
		t.Fatalf("staging replaced the committed share: %v", err)
	}
	if err := s.CommitStaged(f.rec.KeyID, next.Epoch+1); !errors.Is(err, ErrStageEpoch) {
		t.Fatalf("a commit naming another epoch: %v", err)
	}
	if err := s.DiscardStaged(f.rec.KeyID); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitStaged(f.rec.KeyID, next.Epoch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("commit after discard: %v", err)
	}
	if rec, err := s.Get(f.rec.KeyID); err != nil || rec.Epoch != f.rec.Epoch {
		t.Fatalf("discard moved the committed epoch: %+v %v", rec, err)
	}

	if err := s.PutStaged(next, nextShare); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, dir, key)
	if staged, err := s.GetStaged(f.rec.KeyID); err != nil || staged.Epoch != next.Epoch {
		t.Fatalf("stage did not survive a restart: %+v %v", staged, err)
	}
	if err := s.CommitStaged(f.rec.KeyID, next.Epoch); err != nil {
		t.Fatal(err)
	}
	if got, err := readShare(t, s, f.rec.KeyID); err != nil || !bytes.Equal(got, nextShare) {
		t.Fatalf("commit did not install the staged share: %v", err)
	}
	if rec, err := s.Get(f.rec.KeyID); err != nil || rec.Epoch != next.Epoch {
		t.Fatalf("commit did not move the epoch: %+v %v", rec, err)
	}
	if _, err := s.GetStaged(f.rec.KeyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("commit left the stage behind: %v", err)
	}

	later := next
	later.Epoch = next.Epoch + 1
	if err := s.PutStaged(later, randomBytes(t, 512)); err != nil {
		t.Fatal(err)
	}
	discarded, err := s.DiscardAllStaged()
	if err != nil || len(discarded) != 1 || discarded[0] != f.rec.KeyID {
		t.Fatalf("discard all: %v %v", discarded, err)
	}
	if got, err := readShare(t, s, f.rec.KeyID); err != nil || !bytes.Equal(got, nextShare) {
		t.Fatalf("discard all touched the committed share: %v", err)
	}
}
