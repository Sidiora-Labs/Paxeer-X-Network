package attestor_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/dealer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/attestor"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/testsupport"
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
			req, err := attestor.EncodeBundle(b)
			if err != nil {
				t.Fatal(err)
			}
			if req.Threshold != 3 || len(req.PartialPublicKeys) != 5 || len(req.Bks) != 5 || req.ParticipantID != b.ParticipantID {
				t.Fatalf("%s: share bundle fields wrong", c.curve)
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
	client.SetTokenSource(nodes.TokenSource())
	ctx := context.Background()

	key, _ := crypto.HexToECDSA(secpKeyHex)
	want := crypto.PubkeyToAddress(key.PublicKey)
	bundles, pub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	imports, err := client.Import(ctx, "wallet:test:secp256k1", "owner-1", want.Hex(), bundles, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports.Keys) != 5 || imports.SessionID == "" {
		t.Fatalf("%d import responses under session %q", len(imports.Keys), imports.SessionID)
	}
	for _, id := range nodes.IDs {
		if !nodes.Holds(id, "wallet:test:secp256k1") {
			t.Fatalf("node %s holds no share", id)
		}
	}
	if _, err := client.SignPersonal(ctx, "wallet:test:secp256k1", "owner-1", []byte("before refresh")); err == nil {
		t.Fatal("sign before refresh succeeded")
	} else {
		var apiErr *attestor.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "key_not_refreshed" {
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
	sig, err := client.SignPersonal(ctx, "wallet:test:secp256k1", "owner-1", msg)
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
	if _, err := client.Import(ctx, "wallet:test:ed25519", "owner-1", want.Hex(), edBundles, edPub); err != nil {
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
	key, _ := crypto.HexToECDSA(secpKeyHex)
	account := crypto.PubkeyToAddress(key.PublicKey).Hex()
	bundles, pub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	bundles[2].Share.Add(bundles[2].Share, big.NewInt(1))
	_, err = client.Import(ctx, "wallet:tampered", "owner-1", account, bundles, pub)
	var apiErr *attestor.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "key_invalid_share" || apiErr.Category != "key" || apiErr.Node != "n3" {
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
	if _, err := pinned.Import(ctx, "wallet:pinned", "owner-1", account, fresh, pub2); !errors.Is(err, attestor.ErrPinMismatch) {
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

func TestVerificationMessageLayout(t *testing.T) {
	pub := bytes.Repeat([]byte{0x04}, 65)
	var want []byte
	want = append(want, "LX:PAXEER-CEREMONY-VERIFY:v1"...)
	for _, f := range [][]byte{[]byte("wallet:a:secp256k1"), pub, []byte("import-a")} {
		want = binary.BigEndian.AppendUint16(want, uint16(len(f)))
		want = append(want, f...)
	}
	if got := attestor.VerificationMessage("wallet:a:secp256k1", pub, "import-a"); !bytes.Equal(got, want) {
		t.Fatalf("verification message %x want %x", got, want)
	}
}

func TestSignVerificationOverTheOperatorIdentity(t *testing.T) {
	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetTokenSource(nodes.TokenSource())
	ctx := context.Background()

	key, _ := crypto.HexToECDSA(secpKeyHex)
	want := crypto.PubkeyToAddress(key.PublicKey)
	bundles, pub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	imported, err := client.Import(ctx, "wallet:verify:secp256k1", "owner-1", want.Hex(), bundles, pub)
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *attestor.APIError
	if _, err := client.SignVerification(ctx, "wallet:verify:secp256k1", imported.SessionID, pub); !errors.As(err, &apiErr) || apiErr.Code != "key_not_refreshed" {
		t.Fatalf("verification before refresh: %v", err)
	}
	if _, err := client.Refresh(ctx, "wallet:verify:secp256k1", pub); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SignVerification(ctx, "wallet:verify:secp256k1", "another-session", pub); !errors.As(err, &apiErr) || apiErr.Code != "verification_not_imported" || apiErr.Category != "key" {
		t.Fatalf("verification under another import session: %v", err)
	}
	v, err := client.SignVerification(ctx, "wallet:verify:secp256k1", imported.SessionID, pub)
	if err != nil {
		t.Fatal(err)
	}
	uncompressed := crypto.FromECDSAPub(&key.PublicKey)
	if !bytes.Equal(v.Message, attestor.VerificationMessage("wallet:verify:secp256k1", uncompressed, imported.SessionID)) || v.Curve != dealer.Secp256k1 || len(v.AuditSeqs) != attestor.SignQuorum {
		t.Fatalf("secp256k1 verification %+v", v)
	}
	recovered, err := crypto.SigToPub(crypto.Keccak256(v.Message), v.Signature)
	if err != nil || crypto.PubkeyToAddress(*recovered) != want {
		t.Fatalf("verification signature does not recover the imported address: %v", err)
	}
	if _, err := client.SignVerification(ctx, "wallet:verify:secp256k1", imported.SessionID, pub); !errors.As(err, &apiErr) || apiErr.Code != "verification_used" {
		t.Fatalf("second verification: %v", err)
	}

	edBundles, edPub, err := dealer.Split(dealer.Ed25519, edSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	edImported, err := client.Import(ctx, "wallet:verify:ed25519", "owner-1", want.Hex(), edBundles, edPub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(ctx, "wallet:verify:ed25519", edPub); err != nil {
		t.Fatal(err)
	}
	ev, err := client.SignVerification(ctx, "wallet:verify:ed25519", edImported.SessionID, edPub)
	if err != nil {
		t.Fatal(err)
	}
	edKey, _ := hex.DecodeString(edPubHex)
	if !bytes.Equal(ev.Message, attestor.VerificationMessage("wallet:verify:ed25519", edKey, edImported.SessionID)) || ev.Curve != dealer.Ed25519 {
		t.Fatalf("ed25519 verification %+v", ev)
	}
	if !ed25519.Verify(ed25519.PublicKey(edKey), ev.Message, ev.Signature) {
		t.Fatal("ed25519 verification signature does not verify against the imported identity key")
	}
	for _, id := range nodes.IDs[:attestor.SignQuorum] {
		if nodes.Verifications[id] != 2 || nodes.Signs[id] != 0 {
			t.Fatalf("node %s granted %d verifications and %d signatures", id, nodes.Verifications[id], nodes.Signs[id])
		}
	}
	nodes.CorruptSigning("wallet:verify:other")
	otherBundles, otherPub, err := dealer.Split(dealer.Secp256k1, secpSecret(t), client.NodeIDs())
	if err != nil {
		t.Fatal(err)
	}
	other, err := client.Import(ctx, "wallet:verify:other", "owner-1", want.Hex(), otherBundles, otherPub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(ctx, "wallet:verify:other", otherPub); err != nil {
		t.Fatal(err)
	}
	corrupt, err := client.SignVerification(ctx, "wallet:verify:other", other.SessionID, otherPub)
	if err != nil {
		t.Fatal(err)
	}
	if recovered, err := crypto.SigToPub(crypto.Keccak256(corrupt.Message), corrupt.Signature); err != nil || crypto.PubkeyToAddress(*recovered) == want {
		t.Fatalf("corrupted verification still recovers the imported address: %v", err)
	}
}
