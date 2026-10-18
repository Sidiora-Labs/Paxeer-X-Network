package l18

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Actor struct {
	Wallet string
	Roles  map[string]bool
}
type JourneyService struct {
	ledger                         *Ledger
	creator                        *Creator
	key                            []byte
	now                            func() time.Time
	releaseDigest, interfaceDigest string
	sourceStream                   string
}
type RewardPage struct {
	Items      []iface.Reward
	NextCursor *string
	Block      Checkpoint
}
type marketSource struct {
	NFTID, Pool, Creator                 string
	Height                               uint64
	BlockHash, TxHash, TxIndex, LogIndex string
}
type rewardCursor struct {
	Wallet      string     `json:"wallet"`
	Program     string     `json:"program"`
	After       string     `json:"after"`
	Block       Checkpoint `json:"block"`
	Expires     int64      `json:"expires"`
	Resource    string     `json:"resource"`
	SchemaMajor int        `json:"schemaMajor"`
	Interface   string     `json:"interface"`
}

type PreparedRewardData struct {
	iface.RewardClaimData
	ExpiresAt string `json:"expiresAt"`
}
type PreparedReward struct {
	Data        PreparedRewardData `json:"data"`
	GeneratedAt string             `json:"generatedAt"`
	NextCursor  *string            `json:"nextCursor"`
	Quality     string             `json:"quality"`
	RequestID   string             `json:"requestId"`
	Revision    *string            `json:"revision"`
	Watermark   *iface.Watermark   `json:"watermark"`
}

func NewCommittedCreator(ctx context.Context, r RPC, now func() time.Time, committed func(context.Context) (Checkpoint, error), digest string, runtimeHashes map[string]string) (*Creator, error) {
	if r == nil || now == nil || committed == nil || digest != ManifestDigest {
		return nil, ErrUnavailable
	}
	frozen := map[string]string{}
	for address, digest := range runtimeHashes {
		frozen[address] = digest
	}
	c := &Creator{rpc: r, now: now, committed: committed}
	c.attest = func(ctx context.Context, b Checkpoint) error { return verifyRuntime(ctx, r, b, frozen) }
	if _, e := c.checkpoint(ctx); e != nil {
		return nil, e
	}
	return c, nil
}
func verifyRuntime(ctx context.Context, r RPC, b Checkpoint, runtimeHashes map[string]string) error {
	var e error
	for _, target := range []string{NFT, Accumulator, FeesRouter, USDL} {
		expected := runtimeHashes[target]
		if !hash.MatchString(expected) {
			return ErrUnavailable
		}
		var code hexutil.Bytes
		if e = r.CallContext(ctx, &code, "eth_getCode", target, map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
			return e
		}
		if len(code) == 0 || strings.ToLower(crypto.Keccak256Hash(code).Hex()) != expected {
			return ErrUnavailable
		}
		var slot hexutil.Bytes
		if e = r.CallContext(ctx, &slot, "eth_getStorageAt", target, "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
			return e
		}
		if len(slot) != 32 {
			return ErrUnavailable
		}
		implementation := common.BytesToAddress(slot[12:])
		if implementation != (common.Address{}) {
			expectedImplementation := runtimeHashes[strings.ToLower(implementation.Hex())]
			if !hash.MatchString(expectedImplementation) {
				return ErrUnavailable
			}
			var implementationCode hexutil.Bytes
			if e = r.CallContext(ctx, &implementationCode, "eth_getCode", implementation.Hex(), map[string]any{"blockHash": b.Hash, "requireCanonical": true}); e != nil {
				return e
			}
			if len(implementationCode) == 0 || strings.ToLower(crypto.Keccak256Hash(implementationCode).Hex()) != expectedImplementation {
				return ErrUnavailable
			}
		}
	}
	return nil
}
func NewJourneyService(l *Ledger, c *Creator, key []byte, now func() time.Time, releaseDigest, interfaceDigest string) (*JourneyService, error) {
	if l == nil || l.epochGuard == nil || c == nil || c.committed == nil || len(key) < 32 || now == nil || !hexDigest(releaseDigest) || !hexDigest(interfaceDigest) {
		return nil, ErrUnavailable
	}
	return &JourneyService{ledger: l, creator: c, key: append([]byte(nil), key...), now: now, releaseDigest: releaseDigest, interfaceDigest: interfaceDigest}, nil
}
func hexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *JourneyService) market(ctx context.Context, id string) (marketSource, error) {
	var m marketSource
	if _, e := amount(id); e != nil {
		return m, e
	}
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return m, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, `SELECT nft_id::text,pool,creator,height,block_hash,transaction_hash,transaction_index::text,log_index::text FROM indexer.indexer_canonical_creations WHERE nft_id=$1::numeric`, id).Scan(&m.NFTID, &m.Pool, &m.Creator, &m.Height, &m.BlockHash, &m.TxHash, &m.TxIndex, &m.LogIndex)
	if e != nil {
		return m, e
	}
	if !address.MatchString(m.Pool) || !address.MatchString(m.Creator) || !hash.MatchString(m.BlockHash) {
		return m, ErrUnavailable
	}
	return m, tx.Commit(ctx)
}
func (s *JourneyService) Preview(ctx context.Context, actor Actor, id string) (FeePreview, error) {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return FeePreview{}, ErrUnauthorized
	}
	m, e := s.market(ctx, id)
	if e != nil {
		return FeePreview{}, e
	}
	p, e := s.creator.Preview(ctx, id, m.Creator)
	if e != nil {
		return FeePreview{}, e
	}
	if p.Pool != m.Pool || m.Height > p.Block.Number {
		return FeePreview{}, ErrConflict
	}
	if p.Owner != who {
		return FeePreview{}, ErrUnauthorized
	}
	return p, nil
}
func (s *JourneyService) claimedHistory(ctx context.Context, m marketSource, who string, b Checkpoint) (string, error) {
	if b.Number < m.Height || b.Number-m.Height > 100000 {
		return "", fmt.Errorf("%w: canonical claim history exceeds bounded RPC window; indexed history provider required", ErrUnavailable)
	}
	total := new(big.Int)
	seen := map[string]bool{}
	for start := m.Height; start <= b.Number; {
		end := start + 4999
		if end > b.Number {
			end = b.Number
		}
		var logs []struct {
			Address     string         `json:"address"`
			Topics      []string       `json:"topics"`
			Data        hexutil.Bytes  `json:"data"`
			TxHash      string         `json:"transactionHash"`
			BlockHash   string         `json:"blockHash"`
			BlockNumber hexutil.Uint64 `json:"blockNumber"`
			Index       hexutil.Uint64 `json:"logIndex"`
			Removed     bool           `json:"removed"`
		}
		filter := map[string]any{"address": Accumulator, "fromBlock": hexutil.EncodeUint64(start), "toBlock": hexutil.EncodeUint64(end), "topics": []any{accumulator.Events["FeesClaimed"].ID.Hex(), addressTopic(m.Pool), addressTopic(who)}}
		if e := s.creator.rpc.CallContext(ctx, &logs, "eth_getLogs", filter); e != nil {
			return "", e
		}
		if len(logs) > 2000 {
			return "", ErrUnavailable
		}
		for _, log := range logs {
			if log.Removed || strings.ToLower(log.Address) != Accumulator || len(log.Topics) != 3 || log.Topics[0] != accumulator.Events["FeesClaimed"].ID.Hex() || log.Topics[1] != addressTopic(m.Pool) || log.Topics[2] != addressTopic(who) || uint64(log.BlockNumber) < start || uint64(log.BlockNumber) > end || !hash.MatchString(log.TxHash) {
				return "", ErrConflict
			}
			id := fmt.Sprintf("125:%s:%d", log.TxHash, log.Index)
			if seen[id] {
				return "", ErrConflict
			}
			seen[id] = true
			var header struct {
				Hash string `json:"hash"`
			}
			if e := s.creator.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(log.BlockNumber)), false); e != nil {
				return "", e
			}
			if header.Hash != log.BlockHash {
				return "", ErrConflict
			}
			values, e := accumulator.Events["FeesClaimed"].Inputs.NonIndexed().Unpack(log.Data)
			if e != nil || len(values) != 1 {
				return "", ErrConflict
			}
			total.Add(total, values[0].(*big.Int))
		}
		if end == b.Number {
			break
		}
		start = end + 1
	}
	return total.String(), nil
}
func (s *JourneyService) cursor(c rewardCursor) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *JourneyService) decodeCursor(token, who, program string) (rewardCursor, error) {
	var c rewardCursor
	if len(token) > 2048 {
		return c, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, ErrInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, ErrInvalid
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), sig) || json.Unmarshal(raw, &c) != nil || c.Wallet != who || c.Program != program || c.Resource != "rewards" || c.SchemaMajor != 2 || c.Interface != s.interfaceDigest || c.Expires < s.now().Unix() || !hash.MatchString(c.Block.Hash) {
		return c, ErrInvalid
	}
	if _, e = amount(c.After); e != nil {
		return c, e
	}
	return c, nil
}
func (s *JourneyService) Rewards(ctx context.Context, actor Actor, program, cursor string, limit int) (RewardPage, error) {
	out := RewardPage{Items: []iface.Reward{}}
	who, e := wallet(actor.Wallet)
	if e != nil {
		return out, ErrUnauthorized
	}
	if limit < 1 || limit > 100 || program != "" && program != "creator_fees" {
		return out, ErrInvalid
	}
	b, e := s.sourceCheckpoint(ctx)
	if e != nil {
		return out, e
	}
	after := "0"
	if cursor != "" {
		c, e := s.decodeCursor(cursor, who, program)
		if e != nil {
			return out, e
		}
		after = c.After
		b = c.Block
		var header struct {
			Hash string `json:"hash"`
		}
		if e = s.creator.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(b.Number), false); e != nil || header.Hash != b.Hash {
			return out, ErrUnavailable
		}
	}
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT nft_id::text,pool,creator,height,block_hash,transaction_hash,transaction_index::text,log_index::text FROM indexer.indexer_canonical_creations WHERE nft_id>$1::numeric AND height<=$2 ORDER BY nft_id LIMIT $3`, after, b.Number, limit+1)
	if e != nil {
		return out, e
	}
	sources := []marketSource{}
	for rows.Next() {
		var m marketSource
		if e = rows.Scan(&m.NFTID, &m.Pool, &m.Creator, &m.Height, &m.BlockHash, &m.TxHash, &m.TxIndex, &m.LogIndex); e != nil {
			rows.Close()
			return out, e
		}
		sources = append(sources, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	more := len(sources) > limit
	if more {
		sources = sources[:limit]
	}
	for _, m := range sources {
		pinned := *s.creator
		pinned.committed = func(context.Context) (Checkpoint, error) { return b, nil }
		p, e := pinned.Preview(ctx, m.NFTID, m.Creator)
		if e != nil {
			return out, e
		}
		if p.Pool != m.Pool {
			return out, ErrConflict
		}
		claimed, e := s.claimedHistory(ctx, m, who, b)
		if e != nil {
			return out, e
		}
		if p.Owner != who && claimed == "0" {
			continue
		}
		claimableRaw := "0"
		if p.Owner == who {
			if !p.Capability.Available || p.Capability.Strategy != "CLAIM" {
				return out, ErrUnavailable
			}
			claimableRaw = p.ClaimableRaw
		}
		earned, _ := new(big.Int).SetString(claimed, 10)
		claimable, _ := amount(claimableRaw)
		earned.Add(earned, claimable)
		out.Items = append(out.Items, iface.Reward{AssetAddress: USDL, ClaimableRaw: iface.UInt256(claimableRaw), ClaimedRaw: iface.AggregateUInt(claimed), EarnedRaw: iface.AggregateUInt(earned.String()), ProgramId: "creator_fees", RewardId: "creator:" + m.NFTID, Wallet: who, SourcePosition: &iface.ChainPosition{ChainId: "125", Height: iface.UInt256(strconv.FormatUint(m.Height, 10)), BlockHash: m.BlockHash, TransactionHash: m.TxHash, TransactionIndex: iface.UInt256(m.TxIndex), LogIndex: iface.UInt256(m.LogIndex)}})
	}
	out.Block = b
	if more && len(sources) > 0 {
		token := s.cursor(rewardCursor{Wallet: who, Program: program, After: sources[len(sources)-1].NFTID, Block: b, Expires: s.now().Add(15 * time.Minute).Unix(), Resource: "rewards", SchemaMajor: 2, Interface: s.interfaceDigest})
		out.NextCursor = &token
	}
	return out, nil
}
func (s *JourneyService) Prepare(ctx context.Context, actor Actor, key string, request iface.RewardClaimBody, requestID string) ([]byte, error) {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if !requestUUID.MatchString(key) || !requestUUID.MatchString(requestID) {
		return nil, ErrInvalid
	}
	if len(request.RewardIds) != 1 {
		return nil, &CapabilityError{"batch_claim", "Deployed contract supports one NFT per claim"}
	}
	recipient, e := wallet(request.Recipient)
	if e != nil {
		return nil, e
	}
	if recipient != who {
		return nil, &CapabilityError{"alternate_claim_recipient", "Deployed claimFees sends funds to the verified NFT owner"}
	}
	id := strings.TrimPrefix(request.RewardIds[0], "creator:")
	if "creator:"+id != request.RewardIds[0] {
		return nil, ErrUnavailable
	}
	if _, e = amount(id); e != nil {
		return nil, e
	}
	key = strings.ToLower(key)
	digest := fingerprint(struct {
		Actor, Method, Path string
		Body                iface.RewardClaimBody
	}{who, "POST", "/v2/me/rewards:prepare-claim", request})
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, who+":"+key); e != nil {
		return nil, e
	}
	var previousDigest string
	var previous []byte
	var expires time.Time
	var release, contract string
	e = tx.QueryRow(ctx, `SELECT digest,response,expires_at,release_digest,interface_digest FROM l18_requests WHERE actor=$1 AND request_key=$2`, who, key).Scan(&previousDigest, &previous, &expires, &release, &contract)
	if e == nil {
		if previousDigest != digest {
			return nil, ErrConflict
		}
		if !s.now().Before(expires) || release != s.releaseDigest || contract != s.interfaceDigest {
			return nil, &CapabilityError{"expired_claim_preparation", "Claim preparation expired or its admitted deployment changed"}
		}
		var historical, pool string
		if e = tx.QueryRow(ctx, `SELECT creator,pool FROM indexer.indexer_canonical_creations WHERE nft_id=$1::numeric`, id).Scan(&historical, &pool); e != nil {
			return nil, e
		}
		fresh, err := s.creator.PrepareClaim(ctx, id, who, historical)
		if err != nil {
			return nil, err
		}
		if fresh.Preview.Pool != pool {
			return nil, ErrConflict
		}
		return previous, tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return nil, e
	}
	var m marketSource
	e = tx.QueryRow(ctx, `SELECT nft_id::text,pool,creator,height,block_hash,transaction_hash,transaction_index::text,log_index::text FROM indexer.indexer_canonical_creations WHERE nft_id=$1::numeric`, id).Scan(&m.NFTID, &m.Pool, &m.Creator, &m.Height, &m.BlockHash, &m.TxHash, &m.TxIndex, &m.LogIndex)
	if e != nil {
		return nil, e
	}
	call, e := s.creator.PrepareClaim(ctx, id, who, m.Creator)
	if e != nil {
		return nil, e
	}
	if call.Preview.Pool != m.Pool {
		return nil, ErrConflict
	}
	var units hexutil.Uint64
	if e = s.creator.rpc.CallContext(ctx, &units, "eth_estimateGas", map[string]any{"from": who, "to": call.To, "data": call.Data, "value": "0x0"}, map[string]any{"blockHash": call.Preview.Block.Hash, "requireCanonical": true}); e != nil {
		return nil, e
	}
	if units == 0 {
		return nil, ErrUnavailable
	}
	var price hexutil.Big
	if e = s.creator.rpc.CallContext(ctx, &price, "eth_gasPrice"); e != nil {
		return nil, e
	}
	if (*big.Int)(&price).Sign() <= 0 {
		return nil, ErrUnavailable
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(uint64(units)), (*big.Int)(&price))
	if cost.BitLen() > 256 {
		return nil, ErrInvalid
	}
	gas := iface.UInt256(strconv.FormatUint(uint64(units), 10))
	response := PreparedReward{Data: PreparedRewardData{iface.RewardClaimData{EstimatedGasPaxRaw: iface.UInt256(cost.String()), TransactionRequest: iface.TransactionRequest{ChainId: "125", Data: call.Data, From: who, GasLimitRaw: &gas, To: call.To, ValueRaw: "0"}}, time.Unix(call.ExpiresAt, 0).UTC().Format("2006-01-02T15:04:05.000Z")}, GeneratedAt: s.now().UTC().Format("2006-01-02T15:04:05.000Z"), Quality: "complete", RequestID: requestID, Watermark: s.watermark(call.Preview.Block)}
	raw, e := json.Marshal(response)
	if e != nil {
		return nil, e
	}
	if e = iface.ValidateJSON("RewardClaimResponse", raw); e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_requests(actor,request_key,digest,method,path,response,expires_at,release_digest,interface_digest) VALUES($1,$2,$3,'POST','/v2/me/rewards:prepare-claim',$4,$5,$6,$7)`, who, key, digest, raw, time.Unix(call.ExpiresAt, 0), s.releaseDigest, s.interfaceDigest)
	if e != nil {
		return nil, e
	}
	audit := struct {
		Actor, Key, Digest, Release, Interface string
		Block                                  Checkpoint
	}{who, key, digest, s.releaseDigest, s.interfaceDigest, call.Preview.Block}
	if e = emit(ctx, tx, "prepare:"+who+":"+key, "reward.claim_prepared", audit); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO l18_audit(identity,actor,action,reason,payload) VALUES($1,$2,'prepare_claim','wallet signs and broadcasts',$3)`, who+":"+key, who, raw); e != nil {
		return nil, e
	}
	return raw, tx.Commit(ctx)
}

type CommunityAuthority struct {
	State             string     `json:"state"`
	Owner             string     `json:"owner"`
	HistoricalCreator string     `json:"historicalCreator"`
	Sharing           Capability `json:"sharing"`
	Block             Checkpoint `json:"block"`
}

func (s *JourneyService) CommunityAuthority(ctx context.Context, nftID string, approvedMultisigs []string) (CommunityAuthority, error) {
	m, e := s.market(ctx, nftID)
	if e != nil {
		return CommunityAuthority{}, e
	}
	p, e := s.creator.Preview(ctx, nftID, m.Creator)
	if e != nil {
		return CommunityAuthority{}, e
	}
	if p.Pool != m.Pool {
		return CommunityAuthority{}, ErrConflict
	}
	out := CommunityAuthority{State: "proposed", Owner: p.Owner, HistoricalCreator: m.Creator, Sharing: SharingCapability(), Block: p.Block}
	for _, configured := range approvedMultisigs {
		w, e := wallet(configured)
		if e != nil {
			return CommunityAuthority{}, e
		}
		if w == p.Owner {
			out.State = "active"
			return out, nil
		}
	}
	if p.Owner != m.Creator {
		out.State = "proof_pending"
	}
	return out, nil
}
func (s *JourneyService) HolderEntitlement(ctx context.Context, actor Actor, nftID, epoch string) (Entitlement, error) {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return Entitlement{}, ErrUnauthorized
	}
	m, e := s.market(ctx, nftID)
	if e != nil {
		return Entitlement{}, e
	}
	return s.ledger.EpochEntitlement(ctx, s.creator, m.Pool, epoch, who)
}
func (s *JourneyService) PrepareHolderClaim(ctx context.Context, actor Actor, nftID, epoch string) (PreparedEpochCall, error) {
	who, e := wallet(actor.Wallet)
	if e != nil {
		return PreparedEpochCall{}, ErrUnauthorized
	}
	m, e := s.market(ctx, nftID)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	call, e := s.ledger.PrepareEpochClaim(ctx, s.creator, nftID, epoch, who)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	if call.Entitlement.Pool != m.Pool {
		return PreparedEpochCall{}, ErrConflict
	}
	return call, nil
}

func (s *JourneyService) WithSourceStream(stream string) (*JourneyService, error) {
	if s == nil || !identifier.MatchString(stream) {
		return nil, ErrInvalid
	}
	next := *s
	next.sourceStream = stream
	return &next, nil
}
func (s *JourneyService) sourceCheckpoint(ctx context.Context) (Checkpoint, error) {
	if s.sourceStream == "" {
		return Checkpoint{}, fmt.Errorf("%w: canonical creation stream binding required", ErrUnavailable)
	}
	chain, e := s.creator.checkpoint(ctx)
	if e != nil {
		return Checkpoint{}, e
	}
	tx, e := s.ledger.begin(ctx)
	if e != nil {
		return Checkpoint{}, e
	}
	defer tx.Rollback(ctx)
	var raw, typed int64
	e = tx.QueryRow(ctx, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=125 AND stream_id=$1`, s.sourceStream).Scan(&raw, &typed)
	if e != nil {
		return Checkpoint{}, e
	}
	if typed < 1 || raw < typed || uint64(typed) > chain.Number {
		return Checkpoint{}, ErrUnavailable
	}
	var blockHash string
	var fetched, decoded bool
	e = tx.QueryRow(ctx, `SELECT block_hash,fetch_complete,required_decode_complete FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, s.sourceStream, typed).Scan(&blockHash, &fetched, &decoded)
	if e != nil {
		return Checkpoint{}, e
	}
	if !fetched || !decoded || !hash.MatchString(blockHash) {
		return Checkpoint{}, ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return Checkpoint{}, e
	}
	b := Checkpoint{uint64(typed), blockHash}
	pinned := *s.creator
	pinned.committed = func(context.Context) (Checkpoint, error) { return b, nil }
	return pinned.checkpoint(ctx)
}
func (s *JourneyService) watermark(b Checkpoint) *iface.Watermark {
	if b.Number == 0 {
		return nil
	}
	revision := strconv.FormatUint(b.Number, 10)
	return &iface.Watermark{ChainId: "125", Height: iface.UInt256(revision), BlockHash: b.Hash, ProjectionRevision: iface.PositiveUInt256(revision), ObservedAt: s.now().UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
}

type CapabilityError struct {
	Capability string
	Reason     string
}

func (e *CapabilityError) Error() string { return e.Reason }
func (e *CapabilityError) Unwrap() error { return ErrUnavailable }
func (s *JourneyService) Strategy(ctx context.Context, actor Actor, nftID string, selected uint8) (Capability, error) {
	preview, e := s.Preview(ctx, actor, nftID)
	if e != nil {
		return Capability{}, e
	}
	supported, e := s.creator.read(ctx, preview.Block, Accumulator, accumulator, "supportsAirdrop", common.HexToAddress(preview.Pool))
	if e != nil {
		return Capability{}, e
	}
	return StrategyCapability(selected, supported.(bool)), nil
}

type CommunityWatcher struct {
	journey      *JourneyService
	approved     map[string]bool
	policyDigest string
}

func NewCommunityWatcher(journey *JourneyService, approvedMultisigs []string) (*CommunityWatcher, error) {
	if journey == nil || len(approvedMultisigs) > 64 {
		return nil, ErrInvalid
	}
	w := &CommunityWatcher{journey: journey, approved: map[string]bool{}}
	configured := []string{}
	for _, value := range approvedMultisigs {
		actor, e := wallet(value)
		if e != nil {
			return nil, e
		}
		if w.approved[actor] {
			return nil, ErrInvalid
		}
		w.approved[actor] = true
		configured = append(configured, actor)
	}
	sort.Strings(configured)
	w.policyDigest = fingerprint(configured)
	return w, nil
}
func (w *CommunityWatcher) Observe(ctx context.Context, request, nftID string) (CommunityAuthority, error) {
	if !requestUUID.MatchString(request) {
		return CommunityAuthority{}, ErrInvalid
	}
	if _, e := amount(nftID); e != nil {
		return CommunityAuthority{}, e
	}
	tx, e := w.journey.ledger.begin(ctx)
	if e != nil {
		return CommunityAuthority{}, e
	}
	defer tx.Rollback(ctx)
	payload := struct{ NFT, Policy string }{nftID, w.policyDigest}
	duplicate, e := inbox(ctx, tx, "community:"+request, payload)
	if e != nil {
		return CommunityAuthority{}, e
	}
	if duplicate {
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT payload FROM l18_community_history WHERE request_id=$1`, request).Scan(&raw)
		if e != nil {
			return CommunityAuthority{}, e
		}
		var out CommunityAuthority
		if json.Unmarshal(raw, &out) != nil {
			return CommunityAuthority{}, ErrConflict
		}
		return out, tx.Commit(ctx)
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "community:"+nftID); e != nil {
		return CommunityAuthority{}, e
	}
	var historical, pool string
	e = tx.QueryRow(ctx, `SELECT creator,pool FROM indexer.indexer_canonical_creations WHERE nft_id=$1::numeric`, nftID).Scan(&historical, &pool)
	if e != nil {
		return CommunityAuthority{}, e
	}
	preview, e := w.journey.creator.Preview(ctx, nftID, historical)
	if e != nil {
		return CommunityAuthority{}, e
	}
	if preview.Pool != pool {
		return CommunityAuthority{}, ErrConflict
	}
	state := "proposed"
	if preview.Owner != historical {
		state = "proof_pending"
	}
	if w.approved[preview.Owner] {
		state = "active"
	} else {
		var active bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM l18_community_history WHERE nft_id=$1::numeric AND state='active')`, nftID).Scan(&active); e != nil {
			return CommunityAuthority{}, e
		}
		if active {
			state = "revoked"
		}
	}
	out := CommunityAuthority{State: state, Owner: preview.Owner, HistoricalCreator: historical, Sharing: SharingCapability(), Block: preview.Block}
	raw, e := json.Marshal(out)
	if e != nil {
		return CommunityAuthority{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_community_history(request_id,nft_id,state,owner,policy_digest,service_identity,block_number,block_hash,payload) VALUES($1,$2::numeric,$3,$4,$5,$6,$7,$8,$9)`, request, nftID, state, preview.Owner, w.policyDigest, w.journey.ledger.service, preview.Block.Number, preview.Block.Hash, raw)
	if e != nil {
		return CommunityAuthority{}, e
	}
	if e = emit(ctx, tx, "community:"+request, "reward.community_authority", out); e != nil {
		return CommunityAuthority{}, e
	}
	return out, tx.Commit(ctx)
}

func (w *CommunityWatcher) RunOnce(ctx context.Context) error {
	tx, e := w.journey.ledger.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT c.nft_id::text,c.creator,c.pool FROM indexer.indexer_canonical_creations c LEFT JOIN l18_community_scan s ON s.nft_id=c.nft_id ORDER BY s.checked_at NULLS FIRST,c.nft_id LIMIT 32`)
	if e != nil {
		return e
	}
	type source struct{ ID, Creator, Pool string }
	sources := []source{}
	for rows.Next() {
		var item source
		if e = rows.Scan(&item.ID, &item.Creator, &item.Pool); e != nil {
			rows.Close()
			return e
		}
		sources = append(sources, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	for _, item := range sources {
		preview, e := w.journey.creator.Preview(ctx, item.ID, item.Creator)
		if e != nil {
			return e
		}
		if preview.Pool != item.Pool {
			return ErrConflict
		}
		checked, e := w.journey.ledger.begin(ctx)
		if e != nil {
			return e
		}
		var owner string
		e = checked.QueryRow(ctx, `SELECT owner FROM l18_community_history WHERE nft_id=$1::numeric ORDER BY revision DESC LIMIT 1`, item.ID).Scan(&owner)
		if e != nil && e != pgx.ErrNoRows {
			checked.Rollback(ctx)
			return e
		}
		changed := e == pgx.ErrNoRows || owner != preview.Owner
		if e = checked.Commit(ctx); e != nil {
			return e
		}
		if changed {
			if _, e = w.Observe(ctx, uuid.NewString(), item.ID); e != nil {
				return e
			}
		}
		checkpoint, e := w.journey.ledger.begin(ctx)
		if e != nil {
			return e
		}
		if _, e = checkpoint.Exec(ctx, `INSERT INTO l18_community_scan(nft_id) VALUES($1::numeric) ON CONFLICT(nft_id) DO UPDATE SET checked_at=clock_timestamp()`, item.ID); e != nil {
			checkpoint.Rollback(ctx)
			return e
		}
		if e = checkpoint.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
