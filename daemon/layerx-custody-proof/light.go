package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"github.com/sidiora-labs/paxeer-network/layerxproof/codec"
	"golang.org/x/sys/unix"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	rpcclient "github.com/sidiora-labs/paxeer-network/consensus/rpc/client"
	rpchttp "github.com/sidiora-labs/paxeer-network/consensus/rpc/client/http"
	"github.com/sidiora-labs/paxeer-network/consensus/rpc/coretypes"
	"github.com/sidiora-labs/paxeer-network/consensus/types"
	"github.com/sidiora-labs/paxeer-network/custodyproof"
	lxverify "github.com/sidiora-labs/paxeer-network/layerxproof/verify"
	custodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
)

const (
	lightTimeout    = 60 * time.Second
	lightProofTries = 10
	lightProofPause = 1100 * time.Millisecond
	lightRateLimit  = "historical proof rate limited"
)

// depositProof queries the deposit record with its store proof. paxd serves
// one historical proof per second; only that refusal is retried.
func depositProof(ctx context.Context, client *rpchttp.HTTP, depositID [32]byte, height int64) (*coretypes.ResultABCIQuery, error) {
	for try := 1; ; try++ {
		query, err := client.ABCIQueryWithOptions(ctx, "/store/"+custodytypes.StoreKey+"/key", custodytypes.DepositKey(depositID),
			rpcclient.ABCIQueryOptions{Height: height, Prove: true})
		limited := (err != nil && strings.Contains(err.Error(), lightRateLimit)) ||
			(err == nil && query.Response.Code != 0 && strings.Contains(query.Response.Log, lightRateLimit))
		if !limited {
			return query, err
		}
		if try == lightProofTries {
			return nil, fmt.Errorf("deposit proof still rate limited after %d tries", lightProofTries)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lightProofPause):
		}
	}
}

func hex32(name, value string) ([32]byte, error) {
	var out [32]byte
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(decoded) != 32 || bytes.Equal(decoded, out[:]) {
		return out, fmt.Errorf("--%s must be 32 non-zero bytes of hex", name)
	}
	copy(out[:], decoded)
	return out, nil
}

func signedHeader(ctx context.Context, client *rpchttp.HTTP, height int64) (*types.SignedHeader, error) {
	result, err := client.Commit(ctx, &height)
	if err != nil {
		return nil, err
	}
	if !result.CanonicalCommit || result.Header == nil || result.Commit == nil || result.Height != height {
		return nil, fmt.Errorf("height %d has no canonical commit yet", height)
	}
	return &result.SignedHeader, nil
}

func validatorsAt(ctx context.Context, client *rpchttp.HTTP, height int64) (*types.ValidatorSet, error) {
	var pages []coretypes.ResultValidators
	perPage := 100
	for page := 1; ; page++ {
		result, err := client.Validators(ctx, &height, &page, &perPage)
		if err != nil {
			return nil, err
		}
		pages = append(pages, *result)
		if page*perPage >= result.Total || page > custodyproof.MaxValidators/perPage {
			break
		}
	}
	return custodyproof.ValidatorSetFromPages(pages, height)
}

func lightProfile(arguments []string) error {
	flags := flag.NewFlagSet("light-profile", flag.ContinueOnError)
	rpc := flags.String("rpc", "", "Comet RPC URL")
	asset := flags.String("asset", "", "32-byte asset id, hex")
	network := flags.Uint("network-id", 0, "LayerX network id")
	trusted := flags.Int64("trusted-height", 0, "trusted header height")
	period := flags.Uint64("trusting-period-seconds", 0, "trusting period in seconds")
	output := flags.String("output", "", "profile file to write")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *rpc == "" || *output == "" || *trusted < 1 || *network == 0 || *network > 0xffffffff ||
		*period < 1 || *period > custodyproof.LightMaxTrustingPeriod {
		return errors.New("light-profile needs --rpc, --asset, --network-id, --trusted-height, --trusting-period-seconds and --output")
	}
	assetID, err := hex32("asset", *asset)
	if err != nil {
		return err
	}
	client, err := rpchttp.New(*rpc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lightTimeout)
	defer cancel()
	header, err := signedHeader(ctx, client, *trusted)
	if err != nil {
		return err
	}
	validators, err := validatorsAt(ctx, client, *trusted)
	if err != nil {
		return err
	}
	if !bytes.Equal(validators.Hash(), header.ValidatorsHash) {
		return errors.New("trusted header validators hash")
	}
	if err := validators.VerifyCommitLightAllSignatures(header.ChainID, header.Commit.BlockID, header.Height,
		header.Commit); err != nil {
		return fmt.Errorf("trusted header quorum: %w", err)
	}
	profile, err := custodyproof.BuildLightProfile(header, assetID, uint32(*network), *period)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, profile, 0o644)
}

func verifyRegistryMetadata(ctx context.Context, endpoint string, height int64, metadata []custodyproof.LightAssetMetadata) error {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return err
	}
	defer client.Close()
	chain, err := client.ChainID(ctx)
	if err != nil {
		return err
	}
	if !chain.IsUint64() || chain.Uint64() != custodyproof.LightEVMChainID {
		return errors.New("registry metadata EVM chain")
	}
	contractABI, err := abi.JSON(strings.NewReader(`[{
"type":"function","name":"getAsset","inputs":[{"name":"assetId","type":"bytes32"}],"outputs":[{"name":"asset","type":"tuple","components":[{"name":"assetId","type":"bytes32"},{"name":"denom","type":"string"},{"name":"pointer","type":"address"},{"name":"enabled","type":"bool"},{"name":"paused","type":"bool"},{"name":"minimumDeposit","type":"uint256"},{"name":"custodyCap","type":"uint256"},{"name":"custodied","type":"uint256"},{"name":"released","type":"uint256"},{"name":"pending","type":"uint256"}]}]},
{"type":"function","name":"nativeAssetId","inputs":[],"outputs":[{"type":"bytes32"}]},
{"type":"function","name":"assetByPointer","inputs":[{"type":"address"}],"outputs":[{"type":"bytes32"}]}]`))
	if err != nil {
		return err
	}
	block := big.NewInt(height)
	call := func(address common.Address, data []byte) ([]byte, error) {
		return client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, block)
	}
	custody := common.HexToAddress(custodytypes.CustodyAddress)
	for index, entry := range metadata {
		asset, err := hex32("asset_id", entry.AssetID)
		if err != nil {
			return err
		}
		pointer := common.HexToAddress(entry.TokenPointer)
		method := "assetByPointer"
		arguments := []any{pointer}
		if index == 0 {
			method, arguments = "nativeAssetId", nil
		}
		input, err := contractABI.Pack(method, arguments...)
		if err != nil {
			return err
		}
		mapped, err := call(custody, input)
		if err != nil {
			return err
		}
		if len(mapped) != 32 || !bytes.Equal(mapped, asset[:]) {
			return fmt.Errorf("%s approved metadata is not the actual custody mapping", entry.Symbol)
		}
		input, err = contractABI.Pack("getAsset", asset)
		if err != nil {
			return err
		}
		raw, err := call(custody, input)
		if err != nil {
			return err
		}
		values, err := contractABI.Unpack("getAsset", raw)
		if err != nil || len(values) != 1 {
			return fmt.Errorf("%s custody asset record", entry.Symbol)
		}
		mapping := *abi.ConvertType(values[0], new(struct {
			AssetId        [32]byte
			Denom          string
			Pointer        common.Address
			Enabled        bool
			Paused         bool
			MinimumDeposit *big.Int
			CustodyCap     *big.Int
			Custodied      *big.Int
			Released       *big.Int
			Pending        *big.Int
		})).(*struct {
			AssetId        [32]byte
			Denom          string
			Pointer        common.Address
			Enabled        bool
			Paused         bool
			MinimumDeposit *big.Int
			CustodyCap     *big.Int
			Custodied      *big.Int
			Released       *big.Int
			Pending        *big.Int
		})
		if mapping.AssetId != asset || mapping.Pointer != pointer || !mapping.Enabled || mapping.Paused || mapping.Denom == "" {
			return fmt.Errorf("%s custody asset disabled, paused or mismatched", entry.Symbol)
		}
		if index == 0 {
			continue
		}
		decimals, err := call(pointer, common.FromHex("0x313ce567"))
		if err != nil {
			return err
		}
		if len(decimals) != 32 || new(big.Int).SetBytes(decimals).Cmp(new(big.Int).SetUint64(uint64(entry.Decimals))) != 0 {
			return fmt.Errorf("%s decimals metadata mismatch", entry.Symbol)
		}
		symbol, err := call(pointer, common.FromHex("0x95d89b41"))
		if err != nil {
			return err
		}
		var decoded string
		if len(symbol) == 32 {
			length := bytes.IndexByte(symbol, 0)
			if length == -1 {
				length = 32
			}
			if !bytes.Equal(symbol[length:], make([]byte, 32-length)) {
				return errors.New("token symbol padding")
			}
			decoded = string(symbol[:length])
		} else {
			stringType, err := abi.NewType("string", "", nil)
			if err != nil {
				return err
			}
			arguments := abi.Arguments{{Type: stringType}}
			values, err := arguments.Unpack(symbol)
			if err != nil || len(values) != 1 {
				return errors.New("token symbol ABI")
			}
			decoded = values[0].(string)
			canonical, err := arguments.Pack(decoded)
			if err != nil || !bytes.Equal(canonical, symbol) {
				return errors.New("token symbol noncanonical ABI")
			}
		}
		if decoded != entry.Symbol {
			return fmt.Errorf("%s token symbol metadata mismatch", entry.Symbol)
		}
	}
	return nil
}

func lightRegistry(arguments []string) error {
	flags := flag.NewFlagSet("light-registry", flag.ContinueOnError)
	rpc := flags.String("rpc", "", "Comet RPC URL")
	evmRPC := flags.String("evm-rpc", "", "chain 125 EVM RPC URL")
	metadataPath := flags.String("registry", "", "approved four-asset metadata JSON")
	network := flags.Uint("network-id", 0, "LayerX network id")
	trusted := flags.Int64("trusted-height", 0, "trusted header height")
	period := flags.Uint64("trusting-period-seconds", 0, "trusting period in seconds")
	output := flags.String("output", "", "registry file to write")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *rpc == "" || *evmRPC == "" || *metadataPath == "" || *output == "" || *trusted < 1 || *network == 0 || *network > 0xffffffff || *period < 1 || *period > custodyproof.LightMaxTrustingPeriod {
		return errors.New("light-registry needs --rpc, --evm-rpc, --registry, --network-id, --trusted-height, --trusting-period-seconds and --output")
	}
	file, err := os.Open(*metadataPath)
	if err != nil {
		return err
	}
	metadataBytes, err := io.ReadAll(io.LimitReader(file, 8193))
	closeError := file.Close()
	if err != nil {
		return err
	}
	if closeError != nil {
		return closeError
	}
	metadata, err := custodyproof.ParseLightAssetMetadata(metadataBytes)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lightTimeout)
	defer cancel()
	client, err := rpchttp.New(*rpc)
	if err != nil {
		return err
	}
	header, err := signedHeader(ctx, client, *trusted)
	if err != nil {
		return err
	}
	validators, err := validatorsAt(ctx, client, *trusted)
	if err != nil {
		return err
	}
	if !bytes.Equal(validators.Hash(), header.ValidatorsHash) {
		return errors.New("trusted registry header validators hash")
	}
	if err := validators.VerifyCommitLightAllSignatures(header.ChainID, header.Commit.BlockID, header.Height, header.Commit); err != nil {
		return fmt.Errorf("trusted registry header quorum: %w", err)
	}
	if err := verifyRegistryMetadata(ctx, *evmRPC, *trusted, metadata); err != nil {
		return err
	}
	registry, err := custodyproof.BuildLightRegistry(header, metadata, uint32(*network), *period)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, registry, 0o644)
}

func kernelLightTrust(socket string, profile *custodyproof.LightProfile, authorization lxverify.SequencerAuthorization, withSnapshot bool) ([]byte, [32]byte, error) {
	var none [32]byte
	if os.Geteuid() != 4021 {
		return nil, none, errors.New("kernel trust client requires authorized LNI caller UID 4021")
	}
	if socket == "" || len(socket) >= 108 {
		return nil, none, errors.New("kernel trust socket path")
	}
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return nil, none, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(lightTimeout)); err != nil {
		return nil, none, err
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, none, err
	}
	var credential *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credential, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, none, err
	}
	if credentialErr != nil {
		return nil, none, credentialErr
	}
	if credential == nil || credential.Uid != 0 || credential.Pid <= 0 {
		return nil, none, errors.New("kernel trust server peer is not the root owner")
	}
	exchange := func(tag, response uint16, correlation uint64, payload []byte) ([]byte, []byte, error) {
		body := binary.BigEndian.AppendUint16(nil, 1)
		body = binary.BigEndian.AppendUint16(body, 8)
		body = binary.BigEndian.AppendUint16(body, tag)
		body = binary.BigEndian.AppendUint64(body, correlation)
		body = binary.BigEndian.AppendUint32(body, uint32(len(payload)))
		body = append(body, payload...)
		body = binary.BigEndian.AppendUint32(body, 0)
		frame := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
		frame = append(frame, body...)
		if _, err := io.Copy(connection, bytes.NewReader(frame)); err != nil {
			return nil, nil, err
		}
		var prefix [4]byte
		if _, err := io.ReadFull(connection, prefix[:]); err != nil {
			return nil, nil, err
		}
		length := binary.BigEndian.Uint32(prefix[:])
		if length < 22 || length > 1048576+160*1024 {
			return nil, nil, errors.New("kernel trust frame bound")
		}
		reply := make([]byte, int(length))
		if _, err := io.ReadFull(connection, reply); err != nil {
			return nil, nil, err
		}
		if binary.BigEndian.Uint16(reply[:2]) != 1 || binary.BigEndian.Uint16(reply[2:4]) != 8 || binary.BigEndian.Uint64(reply[6:14]) != correlation {
			return nil, nil, errors.New("kernel trust reply version or correlation")
		}
		actual := binary.BigEndian.Uint16(reply[4:6])
		if actual != response {
			return nil, nil, fmt.Errorf("kernel trust response tag %d; refresh authenticated current head if it advanced", actual)
		}
		payloadLength := int(binary.BigEndian.Uint32(reply[14:18]))
		if payloadLength > len(reply)-22 {
			return nil, nil, errors.New("kernel trust reply payload length")
		}
		proofOffset := 18 + payloadLength
		proofLength := int(binary.BigEndian.Uint32(reply[proofOffset : proofOffset+4]))
		if proofLength != len(reply)-proofOffset-4 {
			return nil, nil, errors.New("kernel trust reply proof length")
		}
		return reply[18:proofOffset], reply[proofOffset+4:], nil
	}
	info, proof, err := exchange(1, 2, 0, nil)
	if err != nil {
		return nil, none, err
	}
	if len(info) < 93 || len(proof) != 0 || binary.BigEndian.Uint16(info[:2]) != 1 || binary.BigEndian.Uint16(info[2:4]) != 8 || binary.BigEndian.Uint16(info[4:6]) != custodyproof.LightProtocol || binary.BigEndian.Uint32(info[6:10]) != profile.NetworkID || info[10] < 1 || info[10] > 4 || !bytes.Equal(info[59:91], authorization.PublicKey[:]) {
		return nil, none, errors.New("kernel trust NodeInfo identity")
	}
	cursor := 93
	count := int(binary.BigEndian.Uint16(info[91:93]))
	if count > 64 {
		return nil, none, errors.New("kernel trust capability count")
	}
	previous := ""
	for index := 0; index < count; index++ {
		if cursor+2 > len(info) {
			return nil, none, errors.New("kernel trust capability length")
		}
		length := int(binary.BigEndian.Uint16(info[cursor : cursor+2]))
		cursor += 2
		if length == 0 || length > 64 || cursor+length > len(info) {
			return nil, none, errors.New("kernel trust capability length")
		}
		if !utf8.Valid(info[cursor : cursor+length]) {
			return nil, none, errors.New("kernel trust capability UTF-8")
		}
		capability := string(info[cursor : cursor+length])
		cursor += length
		if capability <= previous {
			return nil, none, errors.New("kernel trust capability order")
		}
		previous = capability
	}
	if cursor != len(info) {
		return nil, none, errors.New("kernel trust NodeInfo trailing bytes")
	}
	batch := binary.BigEndian.Uint64(info[19:27])
	if batch == 0 {
		return nil, none, errors.New("kernel trust has no sealed batch")
	}
	request := binary.BigEndian.AppendUint16(nil, 1)
	request = binary.BigEndian.AppendUint64(request, batch)
	headerBytes, headerProof, err := exchange(12, 13, 1, request)
	if err != nil {
		return nil, none, err
	}
	if len(headerBytes) != codec.BatchHeaderBytes || len(headerProof) != 146 || binary.BigEndian.Uint16(headerProof[:2]) != 1 || !bytes.Equal(headerProof[2:34], authorization.SequencerID[:]) || !bytes.Equal(headerProof[34:66], authorization.PublicKey[:]) {
		return nil, none, errors.New("kernel trust signed batch authority")
	}
	first, last := binary.BigEndian.Uint64(headerProof[66:74]), binary.BigEndian.Uint64(headerProof[74:82])
	if first == 0 || last < first || batch < first || batch > last {
		return nil, none, errors.New("kernel trust owner batch range")
	}
	var signature [64]byte
	copy(signature[:], headerProof[82:146])
	header, err := lxverify.BatchHeader(headerBytes, signature, authorization)
	if err != nil {
		return nil, none, err
	}
	if header.Header.ProtocolVersion != custodyproof.LightProtocol || header.Header.NetworkID != profile.NetworkID || header.Header.BatchNumber != batch || header.Header.LastSequence != binary.BigEndian.Uint64(info[11:19]) {
		return nil, none, custodyproof.ErrLightTrustRefresh
	}
	if !withSnapshot {
		return nil, header.Digest, nil
	}
	request = binary.BigEndian.AppendUint16(nil, 1)
	request = append(request, 6)
	request = append(request, profile.AssetID[:]...)
	request = binary.BigEndian.AppendUint64(request, header.Header.LastSequence)
	request = append(request, header.Digest[:]...)
	request = append(request, header.Header.ResultingStateRoot[:]...)
	payload, snapshot, err := exchange(7, 8, 2, request)
	if err != nil {
		return nil, none, err
	}
	if string(payload) != "LXTS1" || len(snapshot) == 0 {
		return nil, none, errors.New("kernel trust snapshot unavailable")
	}
	return snapshot, header.Digest, nil
}

func lightCredit(arguments []string) error {
	flags := flag.NewFlagSet("light-credit", flag.ContinueOnError)
	rpc := flags.String("rpc", "", "Comet RPC URL")
	profilePath := flags.String("profile", "", "LXBC3 or LXBC4 profile file")
	registryPath := flags.String("registry", "", "LXBR1 custody registry file")
	assetSelector := flags.String("asset", "", "32-byte registry asset id, hex")
	deposit := flags.String("deposit-id", "", "32-byte deposit id, hex")
	owner := flags.String("owner-key", "", "32-byte beneficiary owner ed25519 public key, hex")
	height := flags.Int64("height", 0, "header height N; the record is proven at N-1 (default: latest canonical commit)")
	trustedHeight := flags.Int64("trusted-validators-height", 0, "trusted height T whose next validator set (height T+1) is carried when needed")
	kernelSocket := flags.String("kernel-socket", "", "authenticated production LayerX LNI Unix socket")
	trustPath := flags.String("trust-snapshot", "", "authenticated LXTS1 committed custody trust snapshot")
	sequencerID := flags.String("sequencer-id", "", "independently authenticated LayerX sequencer identity")
	sequencerKey := flags.String("sequencer-key", "", "independently authenticated LayerX sequencer public key")
	firstBatch := flags.Uint64("first-authorized-batch", 0, "first independently authorized sequencer batch")
	lastBatch := flags.Uint64("last-authorized-batch", 0, "last independently authorized sequencer batch")
	currentHeader := flags.String("current-header-hash", "", "independently authenticated current LayerX signed header digest")
	output := flags.String("output", "", "credit file to write")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *rpc == "" || (*profilePath == "" && *registryPath == "") || (*profilePath != "" && *registryPath != "") || (*registryPath != "" && *assetSelector == "") || (*profilePath != "" && *assetSelector != "") || *output == "" || *height < 0 || *trustedHeight < 0 {
		return errors.New("light-credit needs --rpc, --profile, --deposit-id, --owner-key and --output")
	}
	depositID, err := hex32("deposit-id", *deposit)
	if err != nil {
		return err
	}
	ownerKey, err := hex32("owner-key", *owner)
	if err != nil {
		return err
	}
	var profileBytes []byte
	if *registryPath != "" {
		asset, err := hex32("asset", *assetSelector)
		if err != nil {
			return err
		}
		registry, err := os.ReadFile(*registryPath)
		if err != nil {
			return err
		}
		profiles, err := custodyproof.DecodeLightRegistry(registry)
		if err != nil {
			return err
		}
		for _, candidate := range profiles {
			if bytes.Equal(candidate[97:129], asset[:]) {
				profileBytes = candidate
			}
		}
		if profileBytes == nil {
			return errors.New("asset has no approved custody profile")
		}
	} else {
		profileBytes, err = os.ReadFile(*profilePath)
		if err != nil {
			return err
		}
	}
	var profile *custodyproof.LightProfile
	if len(profileBytes) == custodyproof.LightProfileBytes && string(profileBytes[:5]) == custodyproof.AssetLightProfileMagic {
		profile, err = custodyproof.DecodeAssetLightProfile(profileBytes)
	} else {
		profile, err = custodyproof.DecodeLightProfile(profileBytes)
	}
	if err != nil {
		return err
	}
	var snapshot *custodyproof.AuthenticatedLightTrust
	var snapshotBytes []byte
	currentHeight := int64(profile.TrustedHeight)
	currentValidatorsHash := profile.TrustedHash[:]
	if *trustPath != "" && *kernelSocket != "" {
		return errors.New("--trust-snapshot and --kernel-socket are mutually exclusive")
	}
	var kernelAuthority lxverify.SequencerAuthorization
	var selectedKernelHeader [32]byte
	if *trustPath != "" || *kernelSocket != "" {
		id, err := hex32("sequencer-id", *sequencerID)
		if err != nil {
			return err
		}
		key, err := hex32("sequencer-key", *sequencerKey)
		if err != nil {
			return err
		}
		var head [32]byte
		if *kernelSocket != "" {
			if *currentHeader != "" {
				return errors.New("--kernel-socket authenticates current head directly; do not supply --current-header-hash")
			}
			kernelAuthority = lxverify.SequencerAuthorization{SequencerID: id, PublicKey: key, FirstBatchNumber: *firstBatch, LastBatchNumber: *lastBatch}
			if *firstBatch == 0 || *lastBatch < *firstBatch {
				return errors.New("kernel trust requires independently authorized batch bounds")
			}
			snapshotBytes, head, err = kernelLightTrust(*kernelSocket, profile, kernelAuthority, true)
			if err != nil {
				return err
			}
			selectedKernelHeader = head
		} else {
			head, err = hex32("current-header-hash", *currentHeader)
			if err != nil {
				return err
			}
			file, err := os.Open(*trustPath)
			if err != nil {
				return err
			}
			snapshotBytes, err = io.ReadAll(io.LimitReader(file, 2200001))
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		snapshot, err = custodyproof.AuthenticateLightTrust(profileBytes, snapshotBytes, custodyproof.LightTrustAuthority{Authorization: lxverify.SequencerAuthorization{SequencerID: id, PublicKey: key, FirstBatchNumber: *firstBatch, LastBatchNumber: *lastBatch}, CurrentHeaderHash: head})
		if err != nil {
			return err
		}
		trust := snapshot.Trust()
		currentHeight = trust.Height
		currentValidatorsHash = trust.NextValidatorsHash
	} else if *sequencerID != "" || *sequencerKey != "" || *firstBatch != 0 || *lastBatch != 0 || *currentHeader != "" {
		return errors.New("current authority requires --trust-snapshot")
	}
	client, err := rpchttp.New(*rpc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lightTimeout)
	defer cancel()
	if *height == 0 {
		status, err := client.Status(ctx)
		if err != nil {
			return err
		}
		*height = status.SyncInfo.LatestBlockHeight - 1
	}
	if *height < 2 {
		return errors.New("header height must be at least 2")
	}
	evidence := &custodyproof.LightEvidence{}
	if evidence.Header, err = signedHeader(ctx, client, *height); err != nil {
		return err
	}
	if evidence.Validators, err = validatorsAt(ctx, client, *height); err != nil {
		return err
	}
	var matching custodyproof.LightTrustValidators
	if *trustedHeight != 0 && *trustedHeight != currentHeight {
		return errors.New("--trusted-validators-height must match selected authenticated trust")
	}
	if snapshot != nil {
		set, err := validatorsAt(ctx, client, currentHeight+1)
		if err != nil {
			return err
		}
		matching = custodyproof.LightTrustValidators{Height: currentHeight + 1, Validators: set}
	} else if *trustedHeight != 0 && *height > currentHeight+1 && !bytes.Equal(evidence.Header.ValidatorsHash, currentValidatorsHash) {
		if evidence.Trusted, err = validatorsAt(ctx, client, currentHeight+1); err != nil {
			return err
		}
	}
	query, err := depositProof(ctx, client, depositID, *height-1)
	if err != nil {
		return err
	}
	evidence.Deposit = &query.Response
	var credit []byte
	if snapshot != nil {
		credit, err = custodyproof.BuildLightCreditWithTrust(profileBytes, ownerKey, profile.NetworkID, evidence, snapshot, matching, time.Now().UTC())
	} else {
		credit, err = custodyproof.BuildLightCredit(profileBytes, ownerKey, profile.NetworkID, evidence, time.Now().UTC())
	}
	if err == nil && *kernelSocket != "" {
		_, current, readErr := kernelLightTrust(*kernelSocket, profile, kernelAuthority, false)
		if readErr != nil {
			return readErr
		}
		if current != selectedKernelHeader {
			return custodyproof.ErrLightTrustRefresh
		}
	} else if err == nil && snapshot != nil {
		latest, readErr := os.ReadFile(*trustPath)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(latest, snapshotBytes) {
			return custodyproof.ErrLightTrustRefresh
		}
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, credit, 0o644); err != nil {
		return err
	}
	nullifier := custodyproof.LightNullifier(depositID)
	return os.WriteFile(*output+".nullifier", []byte(hex.EncodeToString(nullifier[:])+"\n"), 0o644)
}
