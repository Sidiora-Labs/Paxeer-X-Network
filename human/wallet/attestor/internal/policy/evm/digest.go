package evm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/holiman/uint256"
)

var (
	ErrMalformedTypedData = errors.New("malformed typed data")
	ErrDigestMismatch     = errors.New("claimed digest does not match the recomputed digest")
	ErrMalformedFields    = errors.New("digest fields are malformed")
)

const domainType = "EIP712Domain"

type TypedData struct {
	Data   apitypes.TypedData
	Digest common.Hash
}

func DecodeTypedData(raw []byte) (*TypedData, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var data apitypes.TypedData
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedTypedData, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("%w: trailing data", ErrMalformedTypedData)
	}
	if data.Message == nil {
		return nil, fmt.Errorf("%w: missing message", ErrMalformedTypedData)
	}
	message, err := normaliseNumbers(map[string]any(data.Message))
	if err != nil {
		return nil, err
	}
	data.Message = message.(map[string]any)
	digest, err := TypedDataDigest(data)
	if err != nil {
		return nil, err
	}
	return &TypedData{Data: data, Digest: digest}, nil
}

func normaliseNumbers(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		n, ok := new(big.Int).SetString(v.String(), 10)
		if !ok {
			return nil, fmt.Errorf("%w: number %s is not an integer", ErrMalformedTypedData, v)
		}
		return n, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			n, err := normaliseNumbers(item)
			if err != nil {
				return nil, err
			}
			out[key] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			n, err := normaliseNumbers(item)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	default:
		return value, nil
	}
}

func TypedDataDigest(data apitypes.TypedData) (common.Hash, error) {
	if data.PrimaryType == "" || data.PrimaryType == domainType {
		return common.Hash{}, fmt.Errorf("%w: primary type", ErrMalformedTypedData)
	}
	if _, ok := data.Types[domainType]; !ok {
		return common.Hash{}, fmt.Errorf("%w: missing %s type", ErrMalformedTypedData, domainType)
	}
	fields, ok := data.Types[data.PrimaryType]
	if !ok {
		return common.Hash{}, fmt.Errorf("%w: primary type %q is not defined", ErrMalformedTypedData, data.PrimaryType)
	}
	if data.Message == nil {
		return common.Hash{}, fmt.Errorf("%w: missing message", ErrMalformedTypedData)
	}
	for _, field := range fields {
		if _, ok := data.Message[field.Name]; !ok {
			return common.Hash{}, fmt.Errorf("%w: message field %q is missing", ErrMalformedTypedData, field.Name)
		}
	}
	domainFields := data.Domain.Map()
	if len(domainFields) != len(data.Types[domainType]) {
		return common.Hash{}, fmt.Errorf("%w: domain fields do not match the %s type", ErrMalformedTypedData, domainType)
	}
	for _, field := range data.Types[domainType] {
		if _, ok := domainFields[field.Name]; !ok {
			return common.Hash{}, fmt.Errorf("%w: domain field %q is missing", ErrMalformedTypedData, field.Name)
		}
	}
	digest, _, err := apitypes.TypedDataAndHash(data)
	if err != nil {
		return common.Hash{}, fmt.Errorf("%w: %v", ErrMalformedTypedData, err)
	}
	return common.BytesToHash(digest), nil
}

func PersonalDigest(message []byte) common.Hash {
	prefix := "\x19Ethereum Signed Message:\n" + strconv.Itoa(len(message))
	return crypto.Keccak256Hash([]byte(prefix), message)
}

type BatchCall struct {
	To    common.Address
	Value *big.Int
	Data  []byte
}

type GasQuote struct {
	Sponsor        common.Address
	Token          common.Address
	MaxTokenAmount *big.Int
	TokenAmount    *big.Int
	Deadline       *big.Int
	QuoteNonce     *big.Int
	GasCost        *big.Int
}

type SponsoredBatch struct {
	ChainID *big.Int
	Account common.Address
	Nonce   *big.Int
	Calls   []BatchCall
	Quote   GasQuote
}

var (
	quoteTypeHash = crypto.Keccak256Hash([]byte("Quote(uint256 chainId,address account,address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost)"))
	batchTypeHash = crypto.Keccak256Hash([]byte("SponsoredBatch(uint256 nonce,bytes32 callsHash,bytes32 quoteDigest)"))
	maxUint256    = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

func mustType(name string, components []abi.ArgumentMarshaling) abi.Type {
	t, err := abi.NewType(name, "", components)
	if err != nil {
		panic(err)
	}
	return t
}

var (
	callsArguments = abi.Arguments{{Type: mustType("tuple[]", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})}}
	quoteArguments = abi.Arguments{
		{Type: mustType("bytes32", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("address", nil)},
		{Type: mustType("address", nil)},
		{Type: mustType("address", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("uint256", nil)},
	}
	batchArguments = abi.Arguments{
		{Type: mustType("bytes32", nil)},
		{Type: mustType("uint256", nil)},
		{Type: mustType("bytes32", nil)},
		{Type: mustType("bytes32", nil)},
	}
)

func checkUint(value *big.Int, field string) error {
	if value == nil || value.Sign() < 0 || value.Cmp(maxUint256) > 0 {
		return fmt.Errorf("%w: %s", ErrMalformedFields, field)
	}
	return nil
}

func signedMessageHash(digest common.Hash) common.Hash {
	return crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n32"), digest.Bytes())
}

func quoteDigest(chainID *big.Int, account common.Address, quote GasQuote) (common.Hash, error) {
	for field, value := range map[string]*big.Int{
		"chainId":              chainID,
		"quote.maxTokenAmount": quote.MaxTokenAmount,
		"quote.tokenAmount":    quote.TokenAmount,
		"quote.deadline":       quote.Deadline,
		"quote.quoteNonce":     quote.QuoteNonce,
		"quote.gasCost":        quote.GasCost,
	} {
		if err := checkUint(value, field); err != nil {
			return common.Hash{}, err
		}
	}
	encoded, err := quoteArguments.Pack(quoteTypeHash, chainID, account, quote.Sponsor, quote.Token,
		quote.MaxTokenAmount, quote.TokenAmount, quote.Deadline, quote.QuoteNonce, quote.GasCost)
	if err != nil {
		return common.Hash{}, fmt.Errorf("%w: %v", ErrMalformedFields, err)
	}
	return signedMessageHash(crypto.Keccak256Hash(encoded)), nil
}

func callsHash(calls []BatchCall) (common.Hash, error) {
	type abiCall struct {
		To    common.Address
		Value *big.Int
		Data  []byte
	}
	tuples := make([]abiCall, len(calls))
	for i, call := range calls {
		if err := checkUint(call.Value, fmt.Sprintf("calls[%d].value", i)); err != nil {
			return common.Hash{}, err
		}
		data := call.Data
		if data == nil {
			data = []byte{}
		}
		tuples[i] = abiCall{To: call.To, Value: call.Value, Data: data}
	}
	encoded, err := callsArguments.Pack(tuples)
	if err != nil {
		return common.Hash{}, fmt.Errorf("%w: %v", ErrMalformedFields, err)
	}
	return crypto.Keccak256Hash(encoded), nil
}

func SponsoredBatchDigest(batch SponsoredBatch) (common.Hash, error) {
	if err := checkUint(batch.Nonce, "nonce"); err != nil {
		return common.Hash{}, err
	}
	if len(batch.Calls) == 0 {
		return common.Hash{}, fmt.Errorf("%w: calls", ErrMalformedFields)
	}
	calls, err := callsHash(batch.Calls)
	if err != nil {
		return common.Hash{}, err
	}
	quote, err := quoteDigest(batch.ChainID, batch.Account, batch.Quote)
	if err != nil {
		return common.Hash{}, err
	}
	encoded, err := batchArguments.Pack(batchTypeHash, batch.Nonce, calls, quote)
	if err != nil {
		return common.Hash{}, fmt.Errorf("%w: %v", ErrMalformedFields, err)
	}
	return signedMessageHash(crypto.Keccak256Hash(encoded)), nil
}

func AuthorizationDigest(chainID *big.Int, delegate common.Address, nonce uint64) (common.Hash, error) {
	if err := checkUint(chainID, "chainId"); err != nil {
		return common.Hash{}, err
	}
	auth := types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegate,
		Nonce:   nonce,
	}
	return auth.SigHash(), nil
}

type SponsoredBatchClaim struct {
	Batch         SponsoredBatch
	ClaimedDigest common.Hash
}

func (c *SponsoredBatchClaim) Verify() (common.Hash, error) {
	digest, err := SponsoredBatchDigest(c.Batch)
	if err != nil {
		return common.Hash{}, err
	}
	if digest != c.ClaimedDigest {
		return common.Hash{}, fmt.Errorf("%w: claimed %s recomputed %s", ErrDigestMismatch, c.ClaimedDigest, digest)
	}
	return digest, nil
}

type AuthorizationClaim struct {
	ChainID       *big.Int
	Address       common.Address
	Nonce         uint64
	ClaimedDigest common.Hash
}

func (c *AuthorizationClaim) Verify() (common.Hash, error) {
	digest, err := AuthorizationDigest(c.ChainID, c.Address, c.Nonce)
	if err != nil {
		return common.Hash{}, err
	}
	if digest != c.ClaimedDigest {
		return common.Hash{}, fmt.Errorf("%w: claimed %s recomputed %s", ErrDigestMismatch, c.ClaimedDigest, digest)
	}
	return digest, nil
}

type PersonalMessage struct {
	Message []byte
	Digest  common.Hash
}

func DecodePersonalMessage(message []byte) *PersonalMessage {
	return &PersonalMessage{Message: common.CopyBytes(message), Digest: PersonalDigest(message)}
}

const CustodyDomain = "LX:CUSTODY:v2"

var CustodyAddress = common.HexToAddress("0x0000000000000000000000000000000000001013")
var custodyArguments = abi.Arguments{
	{Type: mustType("address", nil)}, {Type: mustType("uint256", nil)}, {Type: mustType("address", nil)},
	{Type: mustType("uint256", nil)}, {Type: mustType("bytes", nil)}, {Type: mustType("uint64", nil)},
	{Type: mustType("uint64", nil)}, {Type: mustType("uint64", nil)}, {Type: mustType("uint256", nil)}, {Type: mustType("uint256", nil)},
}

type CustodyConsent struct {
	Account     common.Address
	Deadline    uint64
	Transaction *Transaction
	Bytes       []byte
}

func DecodeCustody(raw []byte, chainID *big.Int, account common.Address, now uint64) (*CustodyConsent, error) {
	prefix := []byte(CustodyDomain)
	if len(raw) > 32768 || !bytes.HasPrefix(raw, prefix) {
		return nil, ErrMalformedFields
	}
	values, err := custodyArguments.Unpack(raw[len(prefix):])
	if err != nil || len(values) != 10 {
		return nil, ErrMalformedFields
	}
	canonical, err := custodyArguments.Pack(values...)
	if err != nil || !bytes.Equal(canonical, raw[len(prefix):]) {
		return nil, ErrMalformedFields
	}
	owner := values[0].(common.Address)
	network := values[1].(*big.Int)
	to := values[2].(common.Address)
	value := values[3].(*big.Int)
	data := values[4].([]byte)
	nonce := values[5].(uint64)
	deadline := values[6].(uint64)
	gas := values[7].(uint64)
	fee := values[8].(*big.Int)
	tip := values[9].(*big.Int)
	if owner != account || chainID == nil || network.Cmp(chainID) != 0 || to != CustodyAddress || deadline < now || deadline-now > 600 || gas == 0 || fee.Sign() <= 0 || tip.Sign() < 0 || tip.Cmp(fee) > 0 {
		return nil, ErrMalformedFields
	}
	deposit := crypto.Keccak256([]byte("deposit(bytes32)"))[:4]
	token := crypto.Keccak256([]byte("depositToken(address,uint256,bytes32)"))[:4]
	if len(data) == 36 && bytes.Equal(data[:4], deposit) {
		if value.Sign() <= 0 || bytes.Equal(data[4:], make([]byte, 32)) {
			return nil, ErrMalformedFields
		}
	} else if len(data) == 100 && bytes.Equal(data[:4], token) {
		if value.Sign() != 0 || !bytes.Equal(data[4:16], make([]byte, 12)) || bytes.Equal(data[16:36], make([]byte, 20)) || new(big.Int).SetBytes(data[36:68]).Sign() <= 0 || bytes.Equal(data[68:], make([]byte, 32)) {
			return nil, ErrMalformedFields
		}
	} else {
		return nil, ErrMalformedFields
	}
	tx := &Transaction{Type: types.DynamicFeeTxType, ChainID: network, Nonce: nonce, Gas: gas, GasPrice: new(big.Int).Set(fee), GasTipCap: tip, GasFeeCap: fee, To: &to, Value: value, Data: data}
	return &CustodyConsent{Account: owner, Deadline: deadline, Transaction: tx, Bytes: common.CopyBytes(raw)}, nil
}
func (c *CustodyConsent) Matches(tx *Transaction) bool {
	expected := c.Transaction
	return tx.Type == types.DynamicFeeTxType && tx.ChainID.Cmp(expected.ChainID) == 0 && tx.Nonce == expected.Nonce && tx.Gas == expected.Gas && tx.To != nil && *tx.To == *expected.To && tx.Value.Cmp(expected.Value) == 0 && bytes.Equal(tx.Data, expected.Data) && tx.GasFeeCap.Cmp(expected.GasFeeCap) == 0 && tx.GasTipCap.Cmp(expected.GasTipCap) == 0 && len(tx.AccessList) == 0 && len(tx.Authorizations) == 0
}
