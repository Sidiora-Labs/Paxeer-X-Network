package shadow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// A realistic diff-mode debug_traceBlockByNumber response: an array with one
// entry per transaction, each {result: {pre: {...}, post: {...}}}. The union of
// pre (read) and post (written) slots is the touched set.
const samplePrestateJSON = `[
  {"result": {
    "pre": {
      "0x00000000000000000000000000000000000000aa": {
        "balance": "0x1", "nonce": 7,
        "storage": {
          "0x0000000000000000000000000000000000000000000000000000000000000001": "0x000000000000000000000000000000000000000000000000000000000000002a",
          "0x0000000000000000000000000000000000000000000000000000000000000002": "0x0000000000000000000000000000000000000000000000000000000000000000"
        }
      },
      "0x00000000000000000000000000000000000000bb": {
        "balance": "0x0", "code": "0x6060604052",
        "storage": {
          "0x0000000000000000000000000000000000000000000000000000000000000005": "0x0000000000000000000000000000000000000000000000000000000000000007"
        }
      }
    },
    "post": {
      "0x00000000000000000000000000000000000000aa": {
        "storage": {
          "0x0000000000000000000000000000000000000000000000000000000000000003": "0x0000000000000000000000000000000000000000000000000000000000000009"
        }
      }
    }
  }}
]`

func TestMergePrestateTraces(t *testing.T) {
	var traces []prestateTxTrace
	if err := json.Unmarshal([]byte(samplePrestateJSON), &traces); err != nil {
		t.Fatalf("unmarshal prestate: %v", err)
	}

	touched := mergePrestateTraces(traces)
	byAddr := map[common.Address]TouchedAccount{}
	for _, ta := range touched {
		byAddr[ta.Addr] = ta
	}

	addrAA := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	addrBB := common.HexToAddress("0x00000000000000000000000000000000000000bb")

	aa, ok := byAddr[addrAA]
	if !ok {
		t.Fatal("missing account aa")
	}
	// aa's slots unioned across pre (0x01, 0x02) and post (0x03) = 3.
	if len(aa.Slots) != 3 {
		t.Errorf("aa slots = %d, want 3 (%v)", len(aa.Slots), aa.Slots)
	}
	if aa.CheckCode {
		t.Error("aa has no code; CheckCode should be false")
	}
	if !aa.CheckNonce || !aa.CheckBalance {
		t.Error("every touched account should check nonce and balance")
	}

	bb, ok := byAddr[addrBB]
	if !ok {
		t.Fatal("missing account bb")
	}
	if !bb.CheckCode {
		t.Error("bb carries code; CheckCode should be true")
	}
	if len(bb.Slots) != 1 {
		t.Errorf("bb slots = %d, want 1", len(bb.Slots))
	}
}

// mergePrestateTraces output must be sorted (accounts by address, slots by hash)
// so the same block renders a byte-reproducible report.
func TestMergePrestateTraces_Sorted(t *testing.T) {
	var traces []prestateTxTrace
	if err := json.Unmarshal([]byte(samplePrestateJSON), &traces); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	touched := mergePrestateTraces(traces)
	for i := 1; i < len(touched); i++ {
		if common.Bytes2Hex(touched[i-1].Addr[:]) > common.Bytes2Hex(touched[i].Addr[:]) {
			t.Errorf("accounts not sorted: %s before %s", touched[i-1].Addr.Hex(), touched[i].Addr.Hex())
		}
	}
	for _, ta := range touched {
		for i := 1; i < len(ta.Slots); i++ {
			if common.Bytes2Hex(ta.Slots[i-1][:]) > common.Bytes2Hex(ta.Slots[i][:]) {
				t.Errorf("slots not sorted for %s", ta.Addr.Hex())
			}
		}
	}
}

func TestStaticKeySource(t *testing.T) {
	want := []TouchedAccount{{Addr: testAddr, Slots: []common.Hash{testSlot}, CheckNonce: true}}
	src := StaticKeySource{Accounts: want}
	got, err := src.TouchedAccounts(context.Background(), 1)
	if err != nil {
		t.Fatalf("TouchedAccounts: %v", err)
	}
	if len(got) != 1 || got[0].Addr != testAddr {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
