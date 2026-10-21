package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func TestInsertCardCreatesReferralBinding(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	snap := json.RawMessage(`{"version":1,"ownerAddress":"0xowner"}`)
	card := store.CardRow{
		CardID:       "card-1",
		ShortCode:    "abc1234",
		OwnerAddress: "0xOWNER",
		PoolAddress:  "0xPool",
		TokenAddress: "0xToken",
		Snapshot:     snap,
		CreatedAt:    1000,
	}
	if err := st.InsertCard(ctx, card); err != nil {
		t.Fatalf("insert card: %v", err)
	}

	t.Run("GetCard returns the snapshot with lowercased addresses", func(t *testing.T) {
		got, err := st.GetCard(ctx, "card-1")
		if err != nil || got == nil {
			t.Fatalf("get card: got=%v err=%v", got, err)
		}
		if got.OwnerAddress != "0xowner" || got.PoolAddress != "0xpool" {
			t.Errorf("addresses not lowercased: %+v", got)
		}
		decodeSnapshot := func(raw json.RawMessage) any {
			t.Helper()
			if !json.Valid(raw) {
				t.Fatalf("invalid snapshot JSON: %s", raw)
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var decoded any
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatalf("decode snapshot: %v", err)
			}
			return decoded
		}
		if !reflect.DeepEqual(decodeSnapshot(got.Snapshot), decodeSnapshot(snap)) {
			t.Errorf("snapshot content = %s, want %s", got.Snapshot, snap)
		}
	})

	t.Run("GetCardByShortCode resolves the same card", func(t *testing.T) {
		got, err := st.GetCardByShortCode(ctx, "abc1234")
		if err != nil || got == nil {
			t.Fatalf("get by short code: got=%v err=%v", got, err)
		}
		if got.CardID != "card-1" {
			t.Errorf("cardID = %s", got.CardID)
		}
	})

	t.Run("the referral binding was created", func(t *testing.T) {
		exists, err := st.ShortCodeExists(ctx, "abc1234")
		if err != nil || !exists {
			t.Fatalf("short code exists = %v err = %v", exists, err)
		}
		code, err := st.ShortCodeForCard(ctx, "card-1")
		if err != nil || code != "abc1234" {
			t.Fatalf("short code for card = %q err = %v", code, err)
		}
	})

	t.Run("re-inserting the same card is a no-op", func(t *testing.T) {
		if err := st.InsertCard(ctx, card); err != nil {
			t.Fatalf("re-insert: %v", err)
		}
		var count int
		if err := st.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM pnl.pnl_cards WHERE card_id='card-1'`).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Fatalf("card rows = %d, want 1 (idempotent)", count)
		}
	})

	t.Run("unknown card / short code return nil", func(t *testing.T) {
		c, err := st.GetCard(ctx, "nope")
		if err != nil || c != nil {
			t.Fatalf("unknown card = %v err = %v", c, err)
		}
		c2, err := st.GetCardByShortCode(ctx, "nope")
		if err != nil || c2 != nil {
			t.Fatalf("unknown short code = %v err = %v", c2, err)
		}
	})
}
