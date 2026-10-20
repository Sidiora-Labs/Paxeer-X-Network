package chain

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/readpolicy"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// surfaceRuntime is the launchpad precompile SurfaceRuntime installed at every surface address.
var surfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

func TestPoolSurfaceIdentity(t *testing.T) {
	if got := crypto.Keccak256Hash(surfaceRuntime); got != surfaceRuntimeHash {
		t.Fatalf("surface runtime hash %s, constant %s", got.Hex(), surfaceRuntimeHash.Hex())
	}
	registry := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	beacon := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	pool := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	header := testHeader(7)
	cases := []struct {
		name       string
		registered bool
		code       []byte
		want       string
	}{
		{"registered surface", true, surfaceRuntime, ""},
		{"unregistered surface", false, surfaceRuntime, "pool not registered at selected block"},
		{"registered other code", true, []byte{0x60, 0x00, 0x60, 0x00, 0xf3}, "pool proxy runtime identity mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var storageReads int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage
					Method string
					Params []json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&req)
				var result any
				switch req.Method {
				case "eth_chainId":
					result = "0x7d"
				case "eth_getBlockByNumber":
					result = reportedJSON(header)
				case "eth_call":
					var call struct{ To common.Address }
					json.Unmarshal(req.Params[0], &call)
					if call.To != registry {
						t.Error("membership read not addressed to the PoolRegistry", call.To)
					}
					word := make([]byte, 32)
					if tc.registered {
						word[31] = 1
					}
					result = hexutil.Encode(word)
				case "eth_getCode":
					result = hexutil.Encode(tc.code)
				case "eth_getStorageAt":
					storageReads++
					result = hexutil.Encode(common.LeftPadBytes(beacon.Bytes(), 32))
				default:
					t.Error("unexpected method", req.Method)
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
			}))
			defer srv.Close()
			e, err := ethclient.Dial(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			pin := BlockPin{Number: header.Number.Uint64(), Hash: consensusHash(header), DeploymentVersion: "v-test"}
			c := &Client{primary: e, quarantine: map[*ethclient.Client]bool{}, attestations: map[*ethclient.Client]EndpointAttestation{}, issuedPins: map[BlockPin]struct{}{pin: {}}, publicReads: readpolicy.NewCoalescer(), readPermit: make(chan struct{}, 4)}
			c.verified = &VerifiedConfig{Profile: profile.Profile{DeploymentVersion: "v-test"}, AnchorHeight: pin.Number, AnchorHash: pin.Hash}
			a := &ReadAdapter{client: c, manifest: deployment.Manifest{Roles: map[string]deployment.Role{"PoolRegistry": {Address: registry.Hex()}, "PoolBeacon": {Address: beacon.Hex()}}}, poolProxyRuntimeHash: crypto.Keccak256Hash([]byte("pool proxy runtime"))}
			err = a.identifyPool(context.Background(), pin, pool)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errorChainContains(err, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if storageReads != 0 {
				t.Fatal("beacon slot read for a non-proxy pool")
			}
		})
	}
}

// errorChainContains reports whether err or any error it wraps carries want.
func errorChainContains(err error, want string) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if strings.Contains(err.Error(), want) {
			return true
		}
	}
	return false
}
