package evm

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/holiman/uint256"
)

var testChainID = big.NewInt(125)

type ecdsaKey struct {
	key     *ecdsa.PrivateKey
	address common.Address
}

const testKeyHex = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"

func testKey(t *testing.T) *ecdsaKey {
	t.Helper()
	key, err := crypto.HexToECDSA(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return &ecdsaKey{key: key, address: crypto.PubkeyToAddress(key.PublicKey)}
}

func encodeSigned(t *testing.T, inner types.TxData, chainID *big.Int) (*types.Transaction, []byte) {
	t.Helper()
	signer := types.LatestSignerForChainID(chainID)
	tx, err := types.SignNewTx(testKey(t).key, signer, inner)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return tx, raw
}

func assertView(t *testing.T, tx *types.Transaction, view *Transaction) {
	t.Helper()
	if view.Type != tx.Type() {
		t.Fatalf("type %d want %d", view.Type, tx.Type())
	}
	if view.ChainID.Cmp(testChainID) != 0 {
		t.Fatalf("chain id %v", view.ChainID)
	}
	if view.Nonce != tx.Nonce() || view.Gas != tx.Gas() {
		t.Fatalf("nonce/gas %d/%d want %d/%d", view.Nonce, view.Gas, tx.Nonce(), tx.Gas())
	}
	if view.GasPrice.Cmp(tx.GasPrice()) != 0 || view.GasTipCap.Cmp(tx.GasTipCap()) != 0 || view.GasFeeCap.Cmp(tx.GasFeeCap()) != 0 {
		t.Fatalf("fee fields %v/%v/%v", view.GasPrice, view.GasTipCap, view.GasFeeCap)
	}
	if view.To == nil || *view.To != *tx.To() {
		t.Fatalf("to %v want %v", view.To, tx.To())
	}
	if view.Value.Cmp(tx.Value()) != 0 || !bytes.Equal(view.Data, tx.Data()) {
		t.Fatalf("value/data %v/%x", view.Value, view.Data)
	}
	if len(view.AccessList) != len(tx.AccessList()) {
		t.Fatalf("access list %v want %v", view.AccessList, tx.AccessList())
	}
	for i := range view.AccessList {
		if view.AccessList[i].Address != tx.AccessList()[i].Address || len(view.AccessList[i].StorageKeys) != len(tx.AccessList()[i].StorageKeys) {
			t.Fatalf("access list entry %d differs", i)
		}
	}
	want := types.LatestSignerForChainID(testChainID).Hash(tx)
	if view.SigningDigest != want {
		t.Fatalf("signing digest %s want %s", view.SigningDigest, want)
	}
}

func TestDecodeTransactionTypes(t *testing.T) {
	to := common.HexToAddress("0x3333333333333333333333333333333333333333")
	accessList := types.AccessList{{Address: to, StorageKeys: []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02")}}}
	auth, err := types.SignSetCode(testKey(t).key, types.SetCodeAuthorization{
		ChainID: *uint256.NewInt(125),
		Address: common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Nonce:   9,
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]types.TxData{
		"legacy": &types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(3_000_000_000), Gas: 21_000, To: &to, Value: big.NewInt(1_000), Data: []byte{0xde, 0xad}},
		"access_list": &types.AccessListTx{ChainID: testChainID, Nonce: 2, GasPrice: big.NewInt(4_000_000_000), Gas: 50_000, To: &to,
			Value: big.NewInt(2_000), Data: []byte{0x01}, AccessList: accessList},
		"dynamic_fee": &types.DynamicFeeTx{ChainID: testChainID, Nonce: 3, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(9_000_000_000),
			Gas: 60_000, To: &to, Value: big.NewInt(3_000), Data: []byte{0x02, 0x03}, AccessList: accessList},
		"set_code": &types.SetCodeTx{ChainID: uint256.NewInt(125), Nonce: 4, GasTipCap: uint256.NewInt(1_000_000_000), GasFeeCap: uint256.NewInt(9_000_000_000),
			Gas: 90_000, To: to, Value: uint256.NewInt(4_000), Data: []byte{0x04}, AccessList: accessList, AuthList: []types.SetCodeAuthorization{auth}},
	}
	for name, inner := range cases {
		t.Run(name, func(t *testing.T) {
			tx, raw := encodeSigned(t, inner, testChainID)
			view, err := DecodeTransaction(raw, testChainID)
			if err != nil {
				t.Fatal(err)
			}
			assertView(t, tx, view)
			if name == "set_code" {
				if len(view.Authorizations) != 1 || view.Authorizations[0] != auth {
					t.Fatalf("authorizations %v", view.Authorizations)
				}
			} else if len(view.Authorizations) != 0 {
				t.Fatalf("unexpected authorizations %v", view.Authorizations)
			}
		})
	}
}

func TestDecodeUnsignedTransactions(t *testing.T) {
	to := common.HexToAddress("0x3333333333333333333333333333333333333333")
	legacy := types.NewTx(&types.LegacyTx{Nonce: 5, GasPrice: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(7), V: big.NewInt(125), R: new(big.Int), S: new(big.Int)})
	dynamic := types.NewTx(&types.DynamicFeeTx{ChainID: testChainID, Nonce: 6, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(8)})
	for _, tx := range []*types.Transaction{legacy, dynamic} {
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		view, err := DecodeTransaction(raw, testChainID)
		if err != nil {
			t.Fatal(err)
		}
		assertView(t, tx, view)
	}
	unprotected := types.NewTx(&types.LegacyTx{Nonce: 5, GasPrice: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(7), V: new(big.Int), R: new(big.Int), S: new(big.Int)})
	raw, err := unprotected.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTransaction(raw, testChainID); !errors.Is(err, ErrUnprotected) {
		t.Fatalf("unprotected legacy: %v", err)
	}
}

func TestDecodeTransactionRefusesWrongChain(t *testing.T) {
	to := common.HexToAddress("0x3333333333333333333333333333333333333333")
	other := big.NewInt(126)
	for name, inner := range map[string]types.TxData{
		"legacy":      &types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(1)},
		"dynamic_fee": &types.DynamicFeeTx{ChainID: other, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(1)},
	} {
		_, raw := encodeSigned(t, inner, other)
		if _, err := DecodeTransaction(raw, testChainID); !errors.Is(err, ErrChainID) {
			t.Fatalf("%s: expected chain id refusal, got %v", name, err)
		}
	}
	homestead, err := types.SignNewTx(testKey(t).key, types.HomesteadSigner{}, &types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := homestead.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTransaction(raw, testChainID); !errors.Is(err, ErrUnprotected) {
		t.Fatalf("pre-EIP-155 legacy: %v", err)
	}
	if _, err := DecodeTransaction(raw, nil); !errors.Is(err, ErrMissingChainID) {
		t.Fatalf("missing configured chain: %v", err)
	}
}

func TestDecodeTransactionRefusesUndecodableBytes(t *testing.T) {
	for name, raw := range map[string][]byte{
		"empty":        {},
		"garbage":      {0xff, 0x01, 0x02},
		"truncated":    {0x02, 0xf8, 0x70, 0x01},
		"unknown_type": {0x7f, 0xc0},
	} {
		_, err := DecodeTransaction(raw, testChainID)
		if err == nil {
			t.Fatalf("%s decoded", name)
		}
		if !errors.Is(err, ErrUndecodable) && !errors.Is(err, ErrUnsupportedType) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestDecodeCalldataForEachPrecompile(t *testing.T) {
	did := [32]byte{0xaa}
	beneficiary := [32]byte{0xbb}
	pointer := common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe")
	recipient := common.HexToAddress("0x5555555555555555555555555555555555555555")
	cases := []struct {
		name   string
		method string
		args   []any
		key    string
		want   any
	}{
		{"addr", "bindLayerX", []any{did, []byte{1, 2, 3}}, "didPublicKey", did},
		{"layerxcustody", "depositToken", []any{pointer, big.NewInt(500), beneficiary}, "pointer", pointer},
		{"layerxanchor", "finalize", []any{uint64(42)}, "batchNumber", uint64(42)},
		{"layerxexchange", "placeOrder", []any{did, uint8(1), big.NewInt(100), big.NewInt(3), uint8(0)}, "side", uint8(1)},
		{"layerxbridge", "bridgeOut", []any{uint64(1), pointer, big.NewInt(77), recipient}, "recipient", recipient},
		{"launchpad", "buy", []any{pointer, big.NewInt(10), big.NewInt(9), recipient, big.NewInt(1000)}, "token", pointer},
		{"feetoken", "setFeeDenom", []any{"usid"}, "denom", "usid"},
		{"xweb", "request", []any{uint8(2), []byte("query"), uint64(50_000)}, "callbackGas", uint64(50_000)},
	}
	if len(cases) != len(PrecompileNames()) {
		t.Fatalf("cases cover %d of %d precompiles", len(cases), len(PrecompileNames()))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract, err := PrecompileABI(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := contract.Pack(tc.method, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			address, ok := PrecompileAddress(tc.name)
			if !ok {
				t.Fatalf("no address for %s", tc.name)
			}
			call, err := DecodeCalldata(address, data)
			if err != nil {
				t.Fatal(err)
			}
			if call.Kind != CallPrecompile || call.Destination != tc.name || call.Method != tc.method {
				t.Fatalf("call %+v", call)
			}
			if !bytes.Equal(call.Selector, contract.Methods[tc.method].ID) {
				t.Fatalf("selector %x", call.Selector)
			}
			got := call.Args[tc.key]
			switch want := tc.want.(type) {
			case *big.Int:
				if got.(*big.Int).Cmp(want) != 0 {
					t.Fatalf("%s = %v want %v", tc.key, got, want)
				}
			default:
				if got != tc.want {
					t.Fatalf("%s = %v want %v", tc.key, got, tc.want)
				}
			}
			if _, err := DecodeCalldata(address, data[:len(data)-1]); !errors.Is(err, ErrMalformedCall) {
				t.Fatalf("truncated calldata: %v", err)
			}
		})
	}
	addr, _ := PrecompileAddress("addr")
	if _, err := DecodeCalldata(addr, []byte{0xde, 0xad, 0xbe, 0xef}); !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("unknown precompile method: %v", err)
	}
}

func TestDecodeCalldataTokensAndUnknownDestinations(t *testing.T) {
	erc20, err := PrecompileABI(erc20Name)
	if err != nil {
		t.Fatal(err)
	}
	token := common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe")
	recipient := common.HexToAddress("0x5555555555555555555555555555555555555555")
	for _, method := range []string{"transfer", "approve"} {
		data, err := erc20.Pack(method, recipient, big.NewInt(123_456))
		if err != nil {
			t.Fatal(err)
		}
		call, err := DecodeCalldata(token, data)
		if err != nil {
			t.Fatal(err)
		}
		if call.Kind != CallERC20 || call.Method != method || call.Args["value"].(*big.Int).Cmp(big.NewInt(123_456)) != 0 {
			t.Fatalf("%s decoded as %+v", method, call)
		}
	}
	call, err := DecodeCalldata(recipient, []byte{0x12, 0x34, 0x56, 0x78, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if call.Kind != CallUnknown || !bytes.Equal(call.Selector, []byte{0x12, 0x34, 0x56, 0x78}) {
		t.Fatalf("unknown destination decoded as %+v", call)
	}
	native, err := DecodeCalldata(recipient, nil)
	if err != nil {
		t.Fatal(err)
	}
	if native.Kind != CallNative {
		t.Fatalf("empty calldata decoded as %+v", native)
	}
}

func TestABICopiesMatchRepository(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..", "..")
	sources := map[string]string{erc20Name: filepath.Join(root, "precompiles", "common", "erc20_abi.json")}
	for _, name := range PrecompileNames() {
		sources[name] = filepath.Join(root, "precompiles", name, "abi.json")
	}
	for name, source := range sources {
		want, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := EmbeddedABI(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("abi/%s.json differs from %s", name, source)
		}
	}
}

const mailExample = `{
  "types": {
    "EIP712Domain": [
      {"name": "name", "type": "string"},
      {"name": "version", "type": "string"},
      {"name": "chainId", "type": "uint256"},
      {"name": "verifyingContract", "type": "address"}
    ],
    "Person": [
      {"name": "name", "type": "string"},
      {"name": "wallet", "type": "address"}
    ],
    "Mail": [
      {"name": "from", "type": "Person"},
      {"name": "to", "type": "Person"},
      {"name": "contents", "type": "string"}
    ]
  },
  "primaryType": "Mail",
  "domain": {
    "name": "Ether Mail",
    "version": "1",
    "chainId": 1,
    "verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
  },
  "message": {
    "from": {"name": "Cow", "wallet": "0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},
    "to": {"name": "Bob", "wallet": "0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},
    "contents": "Hello, Bob!"
  }
}`

const permitExample = `{
  "types": {
    "EIP712Domain": [
      {"name": "name", "type": "string"},
      {"name": "version", "type": "string"},
      {"name": "chainId", "type": "uint256"},
      {"name": "verifyingContract", "type": "address"}
    ],
    "Permit": [
      {"name": "owner", "type": "address"},
      {"name": "spender", "type": "address"},
      {"name": "value", "type": "uint256"},
      {"name": "nonce", "type": "uint256"},
      {"name": "deadline", "type": "uint256"}
    ]
  },
  "primaryType": "Permit",
  "domain": {
    "name": "Sidiora",
    "version": "1",
    "chainId": 125,
    "verifyingContract": "0x21f7b20a555199fa73A238B1a91FD0f549068fEe"
  },
  "message": {
    "owner": "0x1111111111111111111111111111111111111111",
    "spender": "0x2222222222222222222222222222222222222222",
    "value": "1000000000000000000000001",
    "nonce": 3,
    "deadline": 1900000000
  }
}`

func TestTypedDataDigest(t *testing.T) {
	mail, err := DecodeTypedData([]byte(mailExample))
	if err != nil {
		t.Fatal(err)
	}
	if want := common.HexToHash("0xbe609aee343fb3c4b28e1df9e632fca64fcfaede20f02e86244efddf30957bd2"); mail.Digest != want {
		t.Fatalf("mail digest %s want %s", mail.Digest, want)
	}
	permit, err := DecodeTypedData([]byte(permitExample))
	if err != nil {
		t.Fatal(err)
	}
	reference := apitypes.TypedData{
		Types:       permit.Data.Types,
		PrimaryType: "Permit",
		Domain:      permit.Data.Domain,
		Message: apitypes.TypedDataMessage{
			"owner":    "0x1111111111111111111111111111111111111111",
			"spender":  "0x2222222222222222222222222222222222222222",
			"value":    "1000000000000000000000001",
			"nonce":    "3",
			"deadline": "1900000000",
		},
	}
	want, _, err := apitypes.TypedDataAndHash(reference)
	if err != nil {
		t.Fatal(err)
	}
	if permit.Digest != common.BytesToHash(want) {
		t.Fatalf("permit digest %s want %x", permit.Digest, want)
	}
}

func TestTypedDataRefusesMalformedInput(t *testing.T) {
	cases := map[string]string{
		"not_json":         `{"types":`,
		"unknown_field":    strings.Replace(mailExample, `"primaryType"`, `"extra": 1, "primaryType"`, 1),
		"missing_primary":  strings.Replace(mailExample, `"primaryType": "Mail"`, `"primaryType": "Letter"`, 1),
		"domain_primary":   strings.Replace(mailExample, `"primaryType": "Mail"`, `"primaryType": "EIP712Domain"`, 1),
		"missing_field":    strings.Replace(mailExample, `"contents": "Hello, Bob!"`, `"body": "Hello, Bob!"`, 1),
		"domain_mismatch":  strings.Replace(mailExample, `"version": "1",`, ``, 1),
		"fractional":       strings.Replace(permitExample, `"nonce": 3`, `"nonce": 3.5`, 1),
		"bad_address":      strings.Replace(mailExample, `"0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"`, `"cow"`, 1),
		"missing_message":  strings.Replace(mailExample, `"message"`, `"ignored"`, 1),
		"trailing_content": mailExample + `{}`,
	}
	for name, raw := range cases {
		if _, err := DecodeTypedData([]byte(raw)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestPersonalDigestAppliesPrefix(t *testing.T) {
	message := []byte("Sign in to the Paxeer X wallet")
	want := crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n30Sign in to the Paxeer X wallet"))
	if got := PersonalDigest(message); got != want {
		t.Fatalf("digest %s want %s", got, want)
	}
	if got := PersonalDigest(message); got != common.BytesToHash(accounts.TextHash(message)) {
		t.Fatalf("digest %s disagrees with go-ethereum", got)
	}
	decoded := DecodePersonalMessage(message)
	if decoded.Digest != want || !bytes.Equal(decoded.Message, message) {
		t.Fatalf("decoded %+v", decoded)
	}
	if PersonalDigest(nil) != crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n0")) {
		t.Fatal("empty message digest")
	}
}

func referenceBatch() SponsoredBatch {
	gasCost, _ := new(big.Int).SetString("1000000000000000000", 10)
	return SponsoredBatch{
		ChainID: big.NewInt(1325),
		Account: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:   big.NewInt(0),
		Calls:   []BatchCall{{To: common.HexToAddress("0x3333333333333333333333333333333333333333"), Value: big.NewInt(0), Data: []byte{0x12, 0x34}}},
		Quote: GasQuote{
			Sponsor:        common.HexToAddress("0x2222222222222222222222222222222222222222"),
			Token:          common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe"),
			MaxTokenAmount: big.NewInt(2_100_000),
			TokenAmount:    big.NewInt(2_000_000),
			Deadline:       big.NewInt(1000),
			QuoteNonce:     big.NewInt(7),
			GasCost:        gasCost,
		},
	}
}

func TestSponsoredBatchDigest(t *testing.T) {
	batch := referenceBatch()
	quote, err := quoteDigest(batch.ChainID, batch.Account, batch.Quote)
	if err != nil {
		t.Fatal(err)
	}
	if want := common.HexToHash("0x6c11f34e7848d98b1ae328fe84bf47223eb14e5274ae04daf3dc64c304c813ba"); quote != want {
		t.Fatalf("quote digest %s want %s", quote, want)
	}
	want := common.HexToHash("0xeba39a1c4de2cb2415a6e26ed362f8237cfe005c8d55ebe8f089cf6f7c636d50")
	digest, err := SponsoredBatchDigest(batch)
	if err != nil {
		t.Fatal(err)
	}
	if digest != want {
		t.Fatalf("batch digest %s want %s", digest, want)
	}
	claim := &SponsoredBatchClaim{Batch: batch, ClaimedDigest: want}
	if got, err := claim.Verify(); err != nil || got != want {
		t.Fatalf("verify %s %v", got, err)
	}
	claim.ClaimedDigest = common.HexToHash("0x01")
	if _, err := claim.Verify(); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mismatched claim: %v", err)
	}
	changed := referenceBatch()
	changed.Quote.TokenAmount = big.NewInt(2_000_001)
	if other, err := SponsoredBatchDigest(changed); err != nil || other == want {
		t.Fatalf("changed quote digest %s %v", other, err)
	}
	missing := referenceBatch()
	missing.Nonce = nil
	if _, err := SponsoredBatchDigest(missing); !errors.Is(err, ErrMalformedFields) {
		t.Fatalf("missing nonce: %v", err)
	}
	empty := referenceBatch()
	empty.Calls = nil
	if _, err := SponsoredBatchDigest(empty); !errors.Is(err, ErrMalformedFields) {
		t.Fatalf("empty calls: %v", err)
	}
}

func TestAuthorizationDigest(t *testing.T) {
	delegate := common.HexToAddress("0x4444444444444444444444444444444444444444")
	digest, err := AuthorizationDigest(testChainID, delegate, 3)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rlp.EncodeToBytes([]any{testChainID, delegate, uint64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if want := crypto.Keccak256Hash([]byte{0x05}, encoded); digest != want {
		t.Fatalf("digest %s want %s", digest, want)
	}
	key := testKey(t)
	signed, err := types.SignSetCode(key.key, types.SetCodeAuthorization{ChainID: *uint256.NewInt(125), Address: delegate, Nonce: 3})
	if err != nil {
		t.Fatal(err)
	}
	if authority, err := signed.Authority(); err != nil || authority != key.address {
		t.Fatalf("authority %s %v", authority, err)
	}
	if signed.SigHash() != digest {
		t.Fatalf("sighash %s want %s", signed.SigHash(), digest)
	}
	claim := &AuthorizationClaim{ChainID: testChainID, Address: delegate, Nonce: 3, ClaimedDigest: digest}
	if _, err := claim.Verify(); err != nil {
		t.Fatal(err)
	}
	claim.Nonce = 4
	if _, err := claim.Verify(); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mismatched claim: %v", err)
	}
	if _, err := AuthorizationDigest(nil, delegate, 3); !errors.Is(err, ErrMalformedFields) {
		t.Fatalf("missing chain id: %v", err)
	}
}
