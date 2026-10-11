package types_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
	"testing"

	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

type vector struct {
	Name          string `json:"name"`
	Origin        uint8  `json:"origin"`
	NetworkID     uint64 `json:"network_id"`
	Requester     string `json:"requester"`
	RequestID     uint64 `json:"request_id"`
	Kind          uint8  `json:"kind"`
	Payload       string `json:"payload"`
	PayloadVector string `json:"payload_vector"`
	PayloadHex    string `json:"payload_hex"`
	PayloadHash   string `json:"payload_hash"`
	ContentDigest string `json:"content_digest"`
	Response      string `json:"response"`
	ResponseHex   string `json:"response_hex"`
	ResponseHash  string `json:"response_hash"`
	FullLength    uint32 `json:"full_length"`
	Preimage      string `json:"preimage"`
	Digest        string `json:"digest"`
	Signer        string `json:"signer"`
	Signature     string `json:"signature"`
}

type vectorFile struct {
	Domain         string   `json:"domain"`
	PreimageLength int      `json:"preimage_length"`
	Vectors        []vector `json:"vectors"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/preimage-vectors.json")
	require.NoError(t, err)
	var file vectorFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&file))
	return file
}

// TestPreimageVectors rebuilds every shared vector byte for byte, checks its
// digest and recovers its signer, and checks ATTESTATION.md carries it. An api
// vector takes its payload from the api vector it names.
func TestPreimageVectors(t *testing.T) {
	file := loadVectors(t)
	apiPayloads := map[string]string{}
	for _, v := range loadApiVectors(t).Vectors {
		apiPayloads[v.Name] = v.Payload
	}
	doc, err := os.ReadFile("../ATTESTATION.md")
	require.NoError(t, err)
	require.Equal(t, types.Domain, file.Domain)
	require.Equal(t, types.PreimageLength, file.PreimageLength)
	require.Equal(t, 188, types.PreimageLength)
	require.Len(t, file.Vectors, 3)
	require.Equal(t, []string{"evm-fetch", "program-search", "evm-api-single"},
		[]string{file.Vectors[0].Name, file.Vectors[1].Name, file.Vectors[2].Name})
	origins := map[uint8]bool{}
	kinds := map[uint8]bool{}
	for _, v := range file.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			origins[v.Origin] = true
			kinds[v.Kind] = true
			if v.Kind == types.KindApi {
				require.Empty(t, v.Payload)
				require.Equal(t, apiPayloads[v.PayloadVector], v.PayloadHex)
			} else {
				require.Empty(t, v.PayloadVector)
				require.Equal(t, hexutil.Encode([]byte(v.Payload)), v.PayloadHex)
			}
			payload, err := hexutil.Decode(v.PayloadHex)
			require.NoError(t, err)
			require.Equal(t, hexutil.Encode([]byte(v.Response)), v.ResponseHex)
			payloadHash := types.Keccak(payload)
			responseHash := types.Keccak([]byte(v.Response))
			require.Equal(t, v.PayloadHash, payloadHash.Hex())
			require.Equal(t, v.ResponseHash, responseHash.Hex())

			attestation := types.Attestation{
				Origin:        v.Origin,
				NetworkID:     new(big.Int).SetUint64(v.NetworkID),
				Requester:     types.Hash32(common.HexToHash(v.Requester)),
				RequestID:     v.RequestID,
				Kind:          v.Kind,
				PayloadHash:   payloadHash,
				ContentDigest: types.Hash32(common.HexToHash(v.ContentDigest)),
				ResponseHash:  responseHash,
				FullLength:    v.FullLength,
			}
			preimage := types.Preimage(attestation)
			require.Len(t, preimage, types.PreimageLength)
			require.Equal(t, v.Preimage, hexutil.Encode(preimage))
			digest := types.Digest(attestation)
			require.Equal(t, v.Digest, digest.Hex())
			require.Contains(t, string(doc), v.Digest)
			require.Contains(t, string(doc), v.Signature)

			signature, err := hexutil.Decode(v.Signature)
			require.NoError(t, err)
			signer, err := bridgetypes.RecoverSigner(bridgetypes.Hash32(digest), signature)
			require.NoError(t, err)
			require.Equal(t, v.Signer, signer.Hex())
		})
	}
	require.True(t, origins[types.OriginEVM])
	require.True(t, origins[types.OriginProgram])
	require.True(t, kinds[types.KindFetch])
	require.True(t, kinds[types.KindSearch])
	require.True(t, kinds[types.KindApi])
}

func TestPreimageVectorsFromKeeper(t *testing.T) {
	file := loadVectors(t)
	for _, v := range file.Vectors {
		if v.Origin != types.OriginEVM {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			testApp := app.Setup(t, false, false, false)
			ctx := testApp.GetContextForDeliverTx(nil).WithBlockHeight(1)
			k, ctx := xwebtestutil.NewKeeper(testApp, ctx)
			require.Equal(t, new(big.Int).SetUint64(v.NetworkID), testApp.EvmKeeper.ChainID(ctx))
			requester, err := hexutil.Decode(v.Requester)
			require.NoError(t, err)
			require.Len(t, requester, 32)
			require.Equal(t, make([]byte, 12), requester[:12])
			address := common.BytesToAddress(requester[12:])
			signer := types.Address20(common.HexToAddress(v.Signer))
			payout := sdk.AccAddress(signer[:])
			genesis := types.DefaultGenesis()
			require.Positive(t, v.RequestID)
			genesis.Nonce = v.RequestID - 1
			genesis.Paused = false
			genesis.Params.Fee = sdk.NewInt(1)
			genesis.Attestors = types.AttestorSet{
				Attestors: []types.Attestor{{Signer: signer, Payout: payout.String()}},
				Threshold: 1,
			}
			require.NoError(t, genesis.Validate())
			k.InitGenesis(ctx, *genesis)
			coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), genesis.Params.Fee))
			payer := testApp.EvmKeeper.GetPaxAddressOrDefault(ctx, address)
			require.NoError(t, testApp.BankKeeper.MintCoins(ctx, "evm", coins))
			require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", payer, coins))
			payload, err := hexutil.Decode(v.PayloadHex)
			require.NoError(t, err)
			id, err := k.Request(ctx, address, v.Kind, payload, 1, genesis.Params.Fee)
			require.NoError(t, err)
			require.Equal(t, v.RequestID, id)
			request, found := k.GetRequest(ctx, id)
			require.True(t, found)
			require.Equal(t, v.PayloadHash, request.PayloadHash.Hex())
			response, err := hexutil.Decode(v.ResponseHex)
			require.NoError(t, err)
			content := types.Hash32(common.HexToHash(v.ContentDigest))
			attested := k.Attestation(ctx, request, response, content, v.FullLength)
			require.Equal(t, v.Requester, attested.Requester.Hex())
			preimage := types.Preimage(attested)
			require.Len(t, preimage, 188)
			require.Equal(t, v.Preimage, hexutil.Encode(preimage))
			require.Equal(t, v.Digest, types.Digest(attested).Hex())
			signature, err := hexutil.Decode(v.Signature)
			require.NoError(t, err)
			changed := append([]byte(nil), response...)
			require.NotEmpty(t, changed)
			changed[0] ^= 1
			_, _, err = k.Fulfil(ctx, id, changed, content, v.FullLength, [][]byte{signature})
			require.ErrorIs(t, err, types.ErrBadSignature)
			pending, found := k.GetRequest(ctx, id)
			require.True(t, found)
			require.Equal(t, request, pending)
			require.Equal(t, genesis.Params.Fee,
				testApp.BankKeeper.GetBalance(ctx, k.ModuleAddress(), sdk.MustGetBaseDenom()).Amount)
			fulfilled, result, err := k.Fulfil(ctx, id, response, content, v.FullLength, [][]byte{signature})
			require.NoError(t, err)
			require.Equal(t, types.StatusFulfilled, fulfilled.Status)
			require.Equal(t, id, result.RequestID)
			require.Equal(t, response, result.Response)
			require.Equal(t, content, result.ContentDigest)
			require.Equal(t, v.FullLength, result.FullLength)
			require.Equal(t, []types.Address20{signer}, result.Signers)
			require.Equal(t, genesis.Params.Fee,
				testApp.BankKeeper.GetBalance(ctx, payout, sdk.MustGetBaseDenom()).Amount)
		})
	}
}

func kernelVectorBytes(t *testing.T, source, symbol string) []byte {
	t.Helper()
	pattern := `(?m)^static const uint8_t ` + regexp.QuoteMeta(symbol) +
		`\[\] = \{\s*((?:0x[0-9a-fA-F]{2}U,\s*)*0x[0-9a-fA-F]{2}U\s*)\};`
	arrays := regexp.MustCompile(pattern).FindAllStringSubmatch(source, -1)
	require.Len(t, arrays, 1, "%s must have exactly one canonical byte array", symbol)
	var encoded strings.Builder
	for _, token := range regexp.MustCompile(`0x([0-9a-fA-F]{2})U`).FindAllStringSubmatch(arrays[0][1], -1) {
		encoded.WriteString(token[1])
	}
	decoded, err := hex.DecodeString(encoded.String())
	require.NoError(t, err)
	return decoded
}

func TestPreimageVectorsMatchKernelBytes(t *testing.T) {
	file := loadVectors(t)
	source, err := os.ReadFile("../../../tests/modules/test_web_intake.c")
	require.NoError(t, err)
	for index, name := range []string{"evm-fetch", "program-search"} {
		t.Run(name, func(t *testing.T) {
			require.Greater(t, len(file.Vectors), index)
			v := file.Vectors[index]
			require.Equal(t, name, v.Name)
			attested := types.Attestation{
				Origin:        v.Origin,
				NetworkID:     new(big.Int).SetUint64(v.NetworkID),
				Requester:     types.Hash32(common.HexToHash(v.Requester)),
				RequestID:     v.RequestID,
				Kind:          v.Kind,
				PayloadHash:   types.Hash32(common.HexToHash(v.PayloadHash)),
				ContentDigest: types.Hash32(common.HexToHash(v.ContentDigest)),
				ResponseHash:  types.Keccak([]byte(v.Response)),
				FullLength:    v.FullLength,
			}
			prefix := fmt.Sprintf("xweb_vector_%d_", index)
			kernelPreimage := kernelVectorBytes(t, string(source), prefix+"preimage")
			require.Len(t, kernelPreimage, 188)
			require.Equal(t, v.Preimage, hexutil.Encode(kernelPreimage))
			require.Equal(t, kernelPreimage, types.Preimage(attested))
			kernelDigest := kernelVectorBytes(t, string(source), prefix+"digest")
			require.Len(t, kernelDigest, 32)
			require.Equal(t, v.Digest, hexutil.Encode(kernelDigest))
			digest := types.Digest(attested)
			require.Equal(t, kernelDigest, digest[:])
			if v.Origin == types.OriginProgram {
				require.Equal(t, make([]byte, 28), kernelPreimage[15:43])
				require.Equal(t, []byte{0, 0, 0, 1}, kernelPreimage[43:47])
				require.Equal(t, attested.Requester[:], kernelPreimage[47:79])
			}
		})
	}
}

func TestPreimageFieldOffsets(t *testing.T) {
	requester := types.Address20(common.HexToAddress("0x00000000000000000000000000000000000a11ce"))
	attestation := types.Attestation{
		Origin:        types.OriginEVM,
		NetworkID:     big.NewInt(713714),
		Requester:     types.EVMRequester(requester),
		RequestID:     0x0102030405060708,
		Kind:          types.KindSearch,
		PayloadHash:   types.Hash32{0xaa},
		ContentDigest: types.Hash32{0xbb},
		ResponseHash:  types.Hash32{0xcc},
		FullLength:    0x0a0b0c0d,
	}
	preimage := types.Preimage(attestation)
	require.Equal(t, "PAXEERX_WEB_V1", string(preimage[:14]))
	require.Equal(t, byte(1), preimage[14])
	require.Equal(t, common.LeftPadBytes(big.NewInt(713714).Bytes(), 32), preimage[15:47])
	require.Equal(t, make([]byte, 12), preimage[47:59])
	require.Equal(t, requester[:], preimage[59:79])
	require.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, preimage[79:87])
	require.Equal(t, byte(2), preimage[87])
	require.Equal(t, byte(0xaa), preimage[88])
	require.Equal(t, byte(0xbb), preimage[120])
	require.Equal(t, byte(0xcc), preimage[152])
	require.Equal(t, []byte{0x0a, 0x0b, 0x0c, 0x0d}, preimage[184:188])

	changed := attestation
	changed.Origin = types.OriginProgram
	require.NotEqual(t, types.Digest(attestation), types.Digest(changed))
	changed = attestation
	changed.FullLength++
	require.NotEqual(t, types.Digest(attestation), types.Digest(changed))
}
