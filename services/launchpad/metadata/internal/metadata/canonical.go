package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

type PostgresCanonicalSource struct {
	Schema            string
	CanonicalStreamID string
}
type canonicalMetadata struct {
	Emitter     string                    `json:"emitter"`
	Name        string                    `json:"name"`
	Symbol      string                    `json:"symbol"`
	Decimals    uint8                     `json:"decimals"`
	TotalSupply string                    `json:"totalSupply"`
	Position    accounting.SourcePosition `json:"position"`
	Provenance  accounting.Provenance     `json:"provenance"`
}
type canonicalCreation struct {
	Token                 string                    `json:"token"`
	Pool                  string                    `json:"pool"`
	Creator               string                    `json:"creator"`
	Factory               string                    `json:"factory"`
	Optical               string                    `json:"optical"`
	NFTID                 string                    `json:"nftId"`
	Position              accounting.SourcePosition `json:"position"`
	BlockTimestampSeconds string                    `json:"blockTimestampSeconds"`
	Provenance            accounting.Provenance     `json:"provenance"`
	Metadata              *canonicalMetadata        `json:"metadata"`
}

const canonicalEventsABI = `[{"type":"event","name":"MarketCreated","anonymous":false,"inputs":[{"name":"token","type":"address","indexed":true},{"name":"pool","type":"address","indexed":true},{"name":"creator","type":"address","indexed":true},{"name":"nftId","type":"uint256","indexed":false},{"name":"optical","type":"address","indexed":false}]},{"type":"event","name":"TokenDeployed","anonymous":false,"inputs":[{"name":"token","type":"address","indexed":true},{"name":"pool","type":"address","indexed":true},{"name":"creator","type":"address","indexed":true},{"name":"salt","type":"bytes32","indexed":false},{"name":"name","type":"string","indexed":false},{"name":"symbol","type":"string","indexed":false},{"name":"decimals","type":"uint8","indexed":false},{"name":"totalSupply","type":"uint256","indexed":false},{"name":"timestamp","type":"uint256","indexed":false},{"name":"blockNumber","type":"uint256","indexed":false}]}]`

func canonicalProvenance(p accounting.Provenance) bool {
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	return p.DeploymentID != "" && p.ABIRevision != "" && p.EmitterCodeEpoch != "" && hash.MatchString(p.ReceiptSHA256) && hash.MatchString(p.HeaderSHA256)
}
func (s PostgresCanonicalSource) table(name string) string {
	return pgx.Identifier{s.Schema, name}.Sanitize()
}
func (s PostgresCanonicalSource) rawEvent(ctx context.Context, tx pgx.Tx, pos accounting.SourcePosition, emitter, name string) (map[string]any, error) {
	if accounting.ValidateSourcePosition(pos) != nil || pos.ChainID != "125" || !addressRE.MatchString(emitter) {
		return nil, ErrUnrepresentable
	}
	var height, transactionIndex, blockHash, actualEmitter string
	var topicsJSON, data []byte
	err := tx.QueryRow(ctx, "SELECT height::text,transaction_index::text,block_hash,emitter,topics,data FROM "+s.table("indexer_raw_logs_v1")+" WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2::bigint", pos.TransactionHash, pos.LogIndex).Scan(&height, &transactionIndex, &blockHash, &actualEmitter, &topicsJSON, &data)
	if err != nil {
		return nil, err
	}
	if height != pos.Height || transactionIndex != pos.TransactionIndex || blockHash != pos.BlockHash || actualEmitter != emitter {
		return nil, ErrUnrepresentable
	}
	var topics []string
	if json.Unmarshal(topicsJSON, &topics) != nil || len(topics) != 4 {
		return nil, ErrUnrepresentable
	}
	parsed, err := abi.JSON(strings.NewReader(canonicalEventsABI))
	if err != nil {
		return nil, err
	}
	event, ok := parsed.Events[name]
	if !ok || topics[0] != event.ID.Hex() {
		return nil, ErrUnrepresentable
	}
	indexed := abi.Arguments{}
	for _, arg := range event.Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	hashes := make([]common.Hash, len(topics)-1)
	for i, t := range topics[1:] {
		if len(t) != 66 || !regexp.MustCompile(`^0x[0-9a-f]{64}$`).MatchString(t) || t[2:26] != strings.Repeat("0", 24) {
			return nil, ErrUnrepresentable
		}
		hashes[i] = common.HexToHash(t)
	}
	values := map[string]any{}
	if err = abi.ParseTopicsIntoMap(values, indexed, hashes); err != nil {
		return nil, ErrUnrepresentable
	}
	if err = event.Inputs.NonIndexed().UnpackIntoMap(values, data); err != nil {
		return nil, ErrUnrepresentable
	}
	ordered, err := event.Inputs.NonIndexed().Unpack(data)
	if err != nil {
		return nil, ErrUnrepresentable
	}
	repacked, err := event.Inputs.NonIndexed().Pack(ordered...)
	if err != nil || !bytes.Equal(data, repacked) {
		return nil, ErrUnrepresentable
	}
	return values, nil
}
func canonicalAddress(v any, expected string) bool {
	a, ok := v.(common.Address)
	return ok && strings.ToLower(a.Hex()) == expected
}
func canonicalUint(v any, expected string) bool {
	n, ok := v.(*big.Int)
	return ok && n.Sign() >= 0 && n.String() == expected
}
func (s PostgresCanonicalSource) ResolveTokenInTx(ctx context.Context, tx pgx.Tx, token string) (V2CanonicalIdentity, error) {
	var zero V2CanonicalIdentity
	if tx == nil || s.CanonicalStreamID == "" || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(s.Schema) || !addressRE.MatchString(token) {
		return zero, ErrUnrepresentable
	}
	var c canonicalCreation
	var payload, provenance []byte
	var height, blockHash, transactionHash, transactionIndex, logIndex, timestamp string
	err := tx.QueryRow(ctx, "SELECT token,pool,creator,factory,optical,nft_id::text,height::text,block_hash,transaction_hash,transaction_index::text,log_index::text,block_timestamp::text,provenance,payload FROM "+s.table("indexer_canonical_creations_v1")+" WHERE token=$1", token).Scan(&c.Token, &c.Pool, &c.Creator, &c.Factory, &c.Optical, &c.NFTID, &height, &blockHash, &transactionHash, &transactionIndex, &logIndex, &timestamp, &provenance, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrMissing
	}
	if err != nil {
		return zero, err
	}
	var stored canonicalCreation
	var proof accounting.Provenance
	if json.Unmarshal(payload, &stored) != nil || json.Unmarshal(provenance, &proof) != nil {
		return zero, ErrUnrepresentable
	}
	position := accounting.SourcePosition{ChainID: "125", Height: height, BlockHash: blockHash, TransactionHash: transactionHash, TransactionIndex: transactionIndex, LogIndex: logIndex}
	if stored.Token != c.Token || stored.Pool != c.Pool || stored.Creator != c.Creator || stored.Factory != c.Factory || stored.Optical != c.Optical || stored.NFTID != c.NFTID || stored.Position != position || stored.BlockTimestampSeconds != timestamp || stored.Provenance != proof || !canonicalProvenance(proof) || proof.PoolCodeEpoch == "" {
		return zero, ErrUnrepresentable
	}
	c = stored
	for _, a := range []string{c.Token, c.Pool, c.Creator, c.Factory} {
		if !addressRE.MatchString(a) || a == "0x"+strings.Repeat("0", 40) {
			return zero, ErrUnrepresentable
		}
	}
	if !addressRE.MatchString(c.Optical) || accounting.ValidateSourcePosition(c.Position) != nil {
		return zero, ErrUnrepresentable
	}
	var complete bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+s.table("indexer_block_completeness_v1")+" WHERE chain_id=125 AND height=$1::bigint AND block_hash=$2 AND stream_id=$3 AND fetch_complete AND required_decode_complete)", height, blockHash, s.CanonicalStreamID).Scan(&complete)
	if err != nil {
		return zero, err
	}
	if !complete {
		return zero, ErrUnrepresentable
	}
	creation, err := s.rawEvent(ctx, tx, c.Position, c.Factory, "MarketCreated")
	if err != nil {
		return zero, err
	}
	if !canonicalAddress(creation["token"], c.Token) || !canonicalAddress(creation["pool"], c.Pool) || !canonicalAddress(creation["creator"], c.Creator) || !canonicalAddress(creation["optical"], c.Optical) || !canonicalUint(creation["nftId"], c.NFTID) {
		return zero, ErrUnrepresentable
	}
	m := c.Metadata
	if m == nil || !canonicalProvenance(m.Provenance) || m.Provenance.DeploymentID != proof.DeploymentID || m.Provenance.ReceiptSHA256 != proof.ReceiptSHA256 || m.Provenance.HeaderSHA256 != proof.HeaderSHA256 || m.Position.ChainID != "125" || m.Position.Height != height || m.Position.BlockHash != blockHash || m.Position.TransactionHash != transactionHash || m.Position.TransactionIndex != transactionIndex {
		return zero, ErrUnrepresentable
	}
	mirror, err := s.rawEvent(ctx, tx, m.Position, m.Emitter, "TokenDeployed")
	if err != nil {
		return zero, err
	}
	decimals, ok := mirror["decimals"].(uint8)
	if !ok || decimals != m.Decimals || m.Decimals != 6 || mirror["name"] != m.Name || mirror["symbol"] != m.Symbol || !canonicalAddress(mirror["token"], c.Token) || !canonicalAddress(mirror["pool"], c.Pool) || !canonicalAddress(mirror["creator"], c.Creator) || !canonicalUint(mirror["totalSupply"], m.TotalSupply) || !canonicalUint(mirror["timestamp"], timestamp) || !canonicalUint(mirror["blockNumber"], height) {
		return zero, ErrUnrepresentable
	}
	raw, err := json.Marshal(map[string]any{"address": c.Token, "name": m.Name, "symbol": m.Symbol, "decimals": m.Decimals, "decimalsQuality": "verified", "totalSupplyRaw": m.TotalSupply, "supplyQuality": "verified", "metadataRevision": "0", "logoAssetId": nil, "sourcePosition": m.Position})
	if err != nil {
		return zero, err
	}
	result, err := iface.DecodeToken(raw)
	if err != nil {
		return zero, ErrUnrepresentable
	}
	if _, err = strconv.ParseInt(timestamp, 10, 64); err != nil {
		return zero, ErrUnrepresentable
	}
	return V2CanonicalIdentity{Token: result, PoolAddress: c.Pool, Creator: c.Creator}, nil
}
