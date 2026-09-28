package envelope_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
)

type tamperVectors struct {
	Cases []struct {
		Name   string `json:"name"`
		Offset int    `json:"offset"`
		Want   string `json:"want"`
	} `json:"cases"`
	Truncations []struct {
		Name   string `json:"name"`
		Length int    `json:"length"`
		Want   string `json:"want"`
	} `json:"truncations"`
}

var wantErr = map[string]error{
	"unsupported_version": envelope.ErrUnsupportedVersion,
	"decrypt":             envelope.ErrDecrypt,
	"format":              envelope.ErrFormat,
	"key_length":          envelope.ErrKeyLength,
}

func loadTamper(t *testing.T) tamperVectors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testsupport.WalletDir(t), "ceremony", "testdata", "tamper.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v tamperVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) == 0 || len(v.Truncations) == 0 {
		t.Fatal("tamper vectors are empty")
	}
	return v
}

func TestOpenEnvelopeDecryptsGatewayEnvelopes(t *testing.T) {
	master, masterB64 := testsupport.MasterKey(t)
	vectors := testsupport.GatewayVectors(t, masterB64, 4)
	for i, v := range vectors {
		if v.Version != envelope.Version1 {
			t.Fatalf("vector %d: gateway wrote version %d", i, v.Version)
		}
		stored := v.Address
		if i%2 == 1 {
			stored = strings.ToLower(v.Address)
		}
		key, err := envelope.OpenEnvelope(v.Envelope, master, stored)
		if err != nil {
			t.Fatalf("vector %d: %v", i, err)
		}
		if key.Address() != common.HexToAddress(v.Address) {
			t.Fatalf("vector %d: address %s, gateway derived %s", i, key.Address().Hex(), v.Address)
		}
		scalar, err := key.Scalar()
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(scalar.FillBytes(make([]byte, 32))) != v.Key {
			t.Fatalf("vector %d: decrypted key differs from the gateway key", i)
		}
		key.Zero()
		if _, err := key.Scalar(); !errors.Is(err, envelope.ErrZeroed) {
			t.Fatalf("vector %d: key readable after Zero: %v", i, err)
		}
	}
}

func TestOpenEnvelopeRefusesTamperedEnvelopes(t *testing.T) {
	master, masterB64 := testsupport.MasterKey(t)
	v := testsupport.GatewayVectors(t, masterB64, 1)[0]
	raw, err := base64.StdEncoding.DecodeString(v.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 61 {
		t.Fatalf("gateway envelope is %d bytes, want 61", len(raw))
	}
	tv := loadTamper(t)
	for _, c := range tv.Cases {
		mutated := append([]byte{}, raw...)
		mutated[c.Offset] ^= 0x01
		_, err := envelope.OpenEnvelope(base64.StdEncoding.EncodeToString(mutated), master, v.Address)
		if !errors.Is(err, wantErr[c.Want]) {
			t.Fatalf("%s: got %v want %v", c.Name, err, wantErr[c.Want])
		}
	}
	for _, c := range tv.Truncations {
		_, err := envelope.OpenEnvelope(base64.StdEncoding.EncodeToString(raw[:c.Length]), master, v.Address)
		if !errors.Is(err, wantErr[c.Want]) {
			t.Fatalf("%s: got %v want %v", c.Name, err, wantErr[c.Want])
		}
	}
	if _, err := envelope.OpenEnvelope("not base64 !", master, v.Address); !errors.Is(err, envelope.ErrFormat) {
		t.Fatalf("bad base64: %v", err)
	}
	other, _ := testsupport.MasterKey(t)
	if _, err := envelope.OpenEnvelope(v.Envelope, other, v.Address); !errors.Is(err, envelope.ErrDecrypt) {
		t.Fatalf("wrong master key: %v", err)
	}
	if _, err := envelope.OpenEnvelope(v.Envelope, master[:31], v.Address); !errors.Is(err, envelope.ErrMasterKey) {
		t.Fatalf("short master key: %v", err)
	}
	if _, err := envelope.OpenEnvelope(v.Envelope, master, "0x1234"); !errors.Is(err, envelope.ErrAddress) {
		t.Fatalf("bad stored address: %v", err)
	}
}

func TestOpenEnvelopeRefusesAddressMismatch(t *testing.T) {
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 2)
	_, err := envelope.OpenEnvelope(vs[0].Envelope, master, vs[1].Address)
	var mm *envelope.MismatchError
	if !errors.As(err, &mm) || !errors.Is(err, envelope.ErrAddressMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	if mm.Stored != common.HexToAddress(vs[1].Address) || mm.Derived != common.HexToAddress(vs[0].Address) {
		t.Fatalf("mismatch error carries %s/%s", mm.Stored.Hex(), mm.Derived.Hex())
	}
	if strings.Contains(err.Error(), vs[0].Key) {
		t.Fatal("error string carries key material")
	}
}

func TestLoadMasterKey(t *testing.T) {
	master, masterB64 := testsupport.MasterKey(t)
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if _, err := envelope.LoadMasterKey(getenv); !errors.Is(err, envelope.ErrMasterKey) {
		t.Fatalf("unset: %v", err)
	}
	env[envelope.EnvMasterKey] = base64.StdEncoding.EncodeToString(master[:16])
	if _, err := envelope.LoadMasterKey(getenv); !errors.Is(err, envelope.ErrMasterKey) {
		t.Fatalf("short: %v", err)
	}
	env[envelope.EnvMasterKey] = "%%%"
	if _, err := envelope.LoadMasterKey(getenv); !errors.Is(err, envelope.ErrMasterKey) {
		t.Fatalf("not base64: %v", err)
	}
	env[envelope.EnvMasterKey] = masterB64
	got, err := envelope.LoadMasterKey(getenv)
	if err != nil || hex.EncodeToString(got) != hex.EncodeToString(master) {
		t.Fatalf("valid key: %v", err)
	}
}
