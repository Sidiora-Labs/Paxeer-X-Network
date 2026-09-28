package attestor_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/dealer"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
)

const (
	secpKeyHex = "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"
	edSeedHex  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	edPubHex   = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
)

var ids = []string{"n1", "n2", "n3", "n4", "n5"}

func secpSecret(t *testing.T) *big.Int {
	t.Helper()
	k, err := crypto.HexToECDSA(secpKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return new(big.Int).Set(k.D)
}

func edSecret(t *testing.T) *big.Int {
	t.Helper()
	seed, _ := hex.DecodeString(edSeedHex)
	d := sha512.Sum512(seed)
	d[0] &= 248
	d[31] &= 127
	d[31] |= 64
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[i] = d[31-i]
	}
	ec, _ := dealer.Curve(dealer.Ed25519).Elliptic()
	s := new(big.Int).SetBytes(be)
	return s.Mod(s, ec.Params().N)
}

func TestPointAndBundleCodec(t *testing.T) {
	seed, _ := hex.DecodeString(edSeedHex)
	if hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)) != edPubHex {
		t.Fatal("ed25519 vector inconsistent")
	}
	key, _ := crypto.HexToECDSA(secpKeyHex)
	cases := []struct {
		curve  dealer.Curve
		secret *big.Int
		pub    string
	}{
		{dealer.Secp256k1, secpSecret(t), hex.EncodeToString(crypto.CompressPubkey(&key.PublicKey))},
		{dealer.Ed25519, edSecret(t), edPubHex},
	}
	for _, c := range cases {
		bundles, pub, err := dealer.Split(c.curve, c.secret, ids)
		if err != nil {
			t.Fatal(err)
		}
		enc, err := attestor.EncodePoint(pub)
		if err != nil || enc != c.pub {
			t.Fatalf("%s: public key encodes to %s want %s (%v)", c.curve, enc, c.pub, err)
		}
		back, err := attestor.DecodePoint(c.curve, enc)
		if err != nil || !back.Equal(pub) {
			t.Fatalf("%s: decode round trip failed: %v", c.curve, err)
		}
		for _, b := range bundles {
			req, err := attestor.EncodeBundle("k", b)
			if err != nil {
				t.Fatal(err)
			}
			if !req.Ceremony || req.Threshold != 3 || len(req.Participants) != 5 || req.PublicKey != c.pub {
				t.Fatalf("%s: import request fields wrong", c.curve)
			}
			got, err := attestor.DecodeBundle(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("%s: decoded bundle invalid: %v", c.curve, err)
			}
			if got.Share.Cmp(b.Share) != 0 || !got.PublicKey.Equal(b.PublicKey) {
				t.Fatalf("%s: decoded bundle differs", c.curve)
			}
		}
	}
	if _, err := attestor.DecodePoint(dealer.Secp256k1, "02"+hex.EncodeToString(make([]byte, 31))); !errors.Is(err, attestor.ErrPoint) {
		t.Fatalf("short point: %v", err)
	}
	if _, err := attestor.DecodePoint(dealer.Ed25519, hex.EncodeToString(make([]byte, 31))); !errors.Is(err, attestor.ErrPoint) {
		t.Fatalf("short ed25519 point: %v", err)
	}
}

func TestClientImportRefreshSignOverMutualTLS(t *testing.T) {
	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	key, _ := crypto.HexToECDSA(secpKeyHex)
	want := crypto.PubkeyToAddress(key.PublicKey)
	bundles, pub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	imports, err := client.Import(ctx, "wallet:test:secp256k1", bundles, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 5 {
		t.Fatalf("%d import responses", len(imports))
	}
	for _, id := range nodes.IDs {
		if !nodes.Holds(id, "wallet:test:secp256k1") {
			t.Fatalf("node %s holds no share", id)
		}
	}
	if _, err := client.SignPersonal(ctx, "wallet:test:secp256k1", []byte("before refresh"), "test"); err == nil {
		t.Fatal("sign before refresh succeeded")
	} else {
		var apiErr *attestor.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "participant_share" {
			t.Fatalf("sign before refresh: %v", err)
		}
	}
	refreshed, err := client.Refresh(ctx, "wallet:test:secp256k1", pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refreshed {
		if r.Epoch != 1 {
			t.Fatalf("epoch %d", r.Epoch)
		}
	}
	msg := []byte("ceremony client test digest")
	sig, err := client.SignPersonal(ctx, "wallet:test:secp256k1", msg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(sig.AuditSeqs) != attestor.SignQuorum {
		t.Fatalf("%d signers", len(sig.AuditSeqs))
	}
	recovered, err := crypto.SigToPub(accounts.TextHash(msg), sig.Ethereum())
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != want {
		t.Fatal("recovered address differs from the imported key")
	}

	edBundles, edPub, err := dealer.Split(dealer.Ed25519, edSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Import(ctx, "wallet:test:ed25519", edBundles, edPub); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(ctx, "wallet:test:ed25519", edPub); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(ctx, "wallet:test:ed25519", pub); !errors.Is(err, attestor.ErrPublicKey) {
		t.Fatalf("refresh against the wrong public key: %v", err)
	}
}

func TestClientRefusesTamperedShareAndWrongPin(t *testing.T) {
	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	bundles, pub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	bundles[2].Share.Add(bundles[2].Share, big.NewInt(1))
	_, err = client.Import(ctx, "wallet:tampered", bundles, pub)
	var apiErr *attestor.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "share_mismatch" || apiErr.Node != "n3" {
		t.Fatalf("tampered share: %v", err)
	}

	cfg := nodes.Config()
	cfg.Nodes[0].Pin = nodes.Pins["n2"]
	pinned, err := attestor.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	fresh, pub2, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pinned.Import(ctx, "wallet:pinned", fresh, pub2); !errors.Is(err, attestor.ErrPinMismatch) {
		t.Fatalf("wrong pin: %v", err)
	}
	if nodes.Holds("n1", "wallet:pinned") {
		t.Fatal("share delivered to a node whose pin did not match")
	}
}

func TestLoadConfig(t *testing.T) {
	nodes := testsupport.StartNodes(t)
	env := nodes.Env()
	getenv := func(k string) string { return env[k] }
	cfg, err := attestor.LoadConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Nodes) != 5 || cfg.Nodes[0].ID != "n1" || cfg.Nodes[0].Pin != nodes.Pins["n1"] {
		t.Fatal("config does not carry the nodes")
	}
	if _, err := attestor.New(cfg); err != nil {
		t.Fatal(err)
	}
	env[attestor.EnvNodePins] = "n1=" + hex.EncodeToString(make([]byte, 32))
	if _, err := attestor.LoadConfig(getenv); !errors.Is(err, attestor.ErrConfig) {
		t.Fatalf("missing pins: %v", err)
	}
	env = nodes.Env()
	delete(env, attestor.EnvTLSCAFile)
	if _, err := attestor.LoadConfig(getenv); !errors.Is(err, attestor.ErrConfig) {
		t.Fatalf("missing CA: %v", err)
	}
	cfg.Nodes = cfg.Nodes[:4]
	if _, err := attestor.New(cfg); !errors.Is(err, attestor.ErrConfig) {
		t.Fatalf("four nodes: %v", err)
	}
}
