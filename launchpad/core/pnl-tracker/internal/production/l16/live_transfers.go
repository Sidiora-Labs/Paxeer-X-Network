package l16

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// rebuildCreationPath requires the creation block of a rebuilt token to hold
// the same factory creation path fresh baseline capture accepts: the
// SidioraERC20 constructor mint to the SidioraFactory, then the factory's seed
// of exactly that supply to the pool, in the indexed creation transaction.
func rebuildCreationPath(moves []movement, start int64, creationHash, creationTx, factory, pool string) error {
	creation := make([]movement, 0, 2)
	for _, m := range moves {
		if m.Height == start && m.BlockHash == creationHash {
			creation = append(creation, m)
		}
	}
	_, err := factoryCreationSupply(creation, creationTx, factory, pool)
	return err
}

func (s *Service) liveRebuildTx(ctx context.Context, tx pgx.Tx, wallet, token string, end int64) (RebuildSnapshot, error) {
	paged := true
	var snapshot RebuildSnapshot
	if !address.MatchString(wallet) || !address.MatchString(token) || wallet == zeroAddress || token == zeroAddress || end <= 0 {
		return snapshot, ErrBaselineUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a := s.authority
	var err error
	if err = a.epochGuard(bounded, tx); err != nil {
		return snapshot, err
	}
	partition := "125:" + wallet + ":" + token
	if _, err = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portfolio:"+a.generation+":"+wallet); err != nil {
		return snapshot, err
	}
	if _, err = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, partition); err != nil {
		return snapshot, err
	}
	owner, epoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return snapshot, err
	}
	var start int64
	var creationHash, pool, creationTx string
	if err = tx.QueryRow(bounded, `SELECT height,block_hash,pool,transaction_hash FROM indexer.indexer_canonical_creations_v1 WHERE token=$1`, token).Scan(&start, &creationHash, &pool, &creationTx); err != nil {
		return snapshot, err
	}
	if start <= 0 || end < start || !paged && end-start > 10000 {
		return snapshot, ErrBaselineUnavailable
	}
	code, err := a.rpc.CodeAt(bounded, common.HexToAddress(token), big.NewInt(start-1))
	if err != nil {
		return snapshot, err
	}
	if len(code) != 0 {
		return snapshot, ErrBaselineUnavailable
	}
	code, err = a.rpc.CodeAtHash(bounded, common.HexToAddress(token), common.HexToHash(creationHash))
	if err != nil {
		return snapshot, err
	}
	if len(code) == 0 {
		return snapshot, ErrBaselineUnavailable
	}
	var moves []movement
	var movementDigest, endHash string
	if paged {
		moves, movementDigest, endHash, err = a.liveMovementPages(bounded, tx, token, start, end)
	} else {
		moves, movementDigest, endHash, err = a.verifiedMovements(bounded, tx, token, start, end)
	}
	if err != nil {
		return snapshot, err
	}
	creation, creationReported, err := reportedHeaderByNumber(bounded, a.rpc, big.NewInt(start))
	if err != nil {
		return snapshot, err
	}
	if creationReported != creationHash {
		return snapshot, delivery.ErrIntegrity
	}
	if err = rebuildCreationPath(moves, start, creationHash, creationTx, a.factory, pool); err != nil {
		return snapshot, err
	}
	var fills []accounting.FillV1
	cursorHeight, cursorTx, cursorLog := start-1, int64(-1), int64(-1)
	for {
		rows, e := tx.Query(bounded, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE (payload->'position'->>'height')::bigint BETWEEN $1 AND $2 AND payload->'token'->>'address'=$3 AND ((payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint)>($4,$5,$6) ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint LIMIT 10000`, start, end, token, cursorHeight, cursorTx, cursorLog)
		if e != nil {
			return snapshot, e
		}
		count := 0
		for rows.Next() {
			var raw []byte
			var digest string
			if e = rows.Scan(&raw, &digest); e != nil {
				rows.Close()
				return snapshot, e
			}
			fill, de := accounting.DecodeFillV1(raw)
			if de != nil {
				rows.Close()
				return snapshot, de
			}
			d, de := accounting.FillDigest(fill)
			if de != nil || string(d) != digest {
				rows.Close()
				return snapshot, delivery.ErrIntegrity
			}
			fills = append(fills, fill)
			count++
			cursorHeight, e = strconv.ParseInt(fill.Position.Height, 10, 64)
			if e != nil {
				rows.Close()
				return snapshot, e
			}
			cursorTx, e = strconv.ParseInt(fill.Position.TransactionIndex, 10, 64)
			if e != nil {
				rows.Close()
				return snapshot, e
			}
			cursorLog, e = strconv.ParseInt(fill.Position.LogIndex, 10, 64)
			if e != nil {
				rows.Close()
				return snapshot, e
			}
			if len(fills) > 1000000 {
				rows.Close()
				return snapshot, ErrBaselineUnavailable
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return snapshot, e
		}
		if count < 10000 {
			break
		}
	}
	settlements := map[string]receiptSettlement{}
	for _, fill := range fills {
		settlement, e := a.liveReceiptSettlement(bounded, tx, fill)
		if e != nil {
			return snapshot, e
		}
		key := settlement.tokenMovement.TransactionHash + ":" + strconv.FormatUint(uint64(settlement.tokenMovement.LogIndex), 10)
		if _, ok := settlements[key]; ok {
			return snapshot, delivery.ErrIntegrity
		}
		settlements[key] = settlement
	}

	contractAddress := common.HexToAddress(token)
	decimalRaw, e := a.rpc.CallContractAtHash(bounded, ethereum.CallMsg{To: &contractAddress, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}, common.HexToHash(endHash))
	if e != nil {
		return snapshot, e
	}
	if len(decimalRaw) != 32 || new(big.Int).SetBytes(decimalRaw).Cmp(big.NewInt(255)) > 0 {
		return snapshot, delivery.ErrIntegrity
	}
	tokenDecimals := int(new(big.Int).SetBytes(decimalRaw).Int64())
	records := make([]EconomicRecord, 0)
	state := Empty()
	last := ""
	headers := map[string]time.Time{}
	gasRecorded := map[string]EconomicRecord{}
	for _, m := range moves {
		if m.From != wallet && m.To != wallet {
			continue
		}
		at, ok := headers[m.BlockHash]
		if !ok {
			h, e := a.rpc.HeaderByHash(bounded, common.HexToHash(m.BlockHash))
			if e != nil {
				return snapshot, e
			}
			if h == nil || h.Number == nil || h.Number.Int64() != m.Height || h.Time > 1<<63-1 {
				return snapshot, delivery.ErrIntegrity
			}
			at = time.Unix(int64(h.Time), 0).UTC()
			headers[m.BlockHash] = at
		}
		key := m.TransactionHash + ":" + strconv.FormatUint(uint64(m.LogIndex), 10)
		var settlement *receiptSettlement
		if found, ok := settlements[key]; ok {
			copy := found
			settlement = &copy
		}
		movementBytes, e := json.Marshal(m)
		if e != nil {
			return snapshot, e
		}
		record, include, e := normalizeMovement(wallet, token, m, settlement, at, delivery.Hash(movementBytes))
		if e != nil {
			return snapshot, e
		}
		if !include {
			continue
		}
		record.TokenDecimals = &tokenDecimals
		record.Leg.Predecessor = last
		if record.GasPAXRaw != "" && record.Leg.Sender == wallet {
			if prior, ok := gasRecorded[m.TransactionHash]; ok {
				if prior.GasPAXRaw != record.GasPAXRaw || prior.BlockHash != record.BlockHash || prior.SourceDigest != record.SourceDigest || prior.Leg.Height != record.Leg.Height || prior.Leg.TransactionIndex != record.Leg.TransactionIndex || !prior.CommittedAt.Equal(record.CommittedAt) {
					return snapshot, delivery.ErrIntegrity
				}
				record.GasPAXRaw = "0"
			} else {
				gasRecorded[m.TransactionHash] = record
			}
		}
		if record.Leg.Kind == "sell" || record.Leg.Kind == "outgoing" || record.Leg.Kind == "burn" {
			var allocation Disposal
			state, allocation, e = Dispose(state, record.Leg.Quantity, record.Leg.ActualUSDL)
			record.Allocation = &allocation
		} else {
			state, e = Apply(state, record.Leg)
		}
		if e != nil {
			return snapshot, e
		}
		record.State = state
		last, e = record.Leg.Identity.Key()
		if e != nil {
			return snapshot, e
		}
		records = append(records, record)
		if !paged && len(records) > 10000 || len(records) > 1000000 {
			return snapshot, ErrBaselineUnavailable
		}
	}
	firstBalance, err := a.observedBalance(bounded, token, wallet, endHash)
	if err != nil {
		return snapshot, err
	}
	chain, err := s.second.ChainID(bounded)
	if err != nil {
		return snapshot, err
	}
	if chain.Cmp(big.NewInt(125)) != 0 {
		return snapshot, delivery.ErrIntegrity
	}
	_, otherHash, err := reportedHeaderByNumber(bounded, s.second, big.NewInt(end))
	if err != nil {
		return snapshot, err
	}
	data := append(append([]byte(nil), balanceSelector...), common.LeftPadBytes(common.HexToAddress(wallet).Bytes(), 32)...)
	contract := common.HexToAddress(token)
	rawBalance, err := s.second.CallContractAtHash(bounded, ethereum.CallMsg{To: &contract, Data: data}, common.HexToHash(endHash))
	if err != nil {
		return snapshot, err
	}
	if len(rawBalance) != 32 {
		return snapshot, delivery.ErrIntegrity
	}
	secondBalance := new(big.Int).SetBytes(rawBalance)
	values, err := state.values()
	if err != nil {
		return snapshot, err
	}
	snapshot = RebuildSnapshot{Generation: a.generation, Partition: partition, Algorithm: Algorithm, ReleaseDigest: s.release, CreationHeight: start, Height: end, BlockHash: endHash, MovementDigest: movementDigest, SourceOwner: owner, SourceEpoch: epoch, NodeIDs: s.nodeIDs, NodeBalances: [2]string{firstBalance.String(), secondBalance.String()}, State: state, LastIdentity: last}
	if otherHash != endHash || firstBalance.Cmp(secondBalance) != 0 || firstBalance.Cmp(add(values[0], values[1])) != 0 {
		payload, _ := json.Marshal(snapshot)
		if _, err = tx.Exec(bounded, `INSERT INTO `+a.table("inventory_l16_quarantine_v2")+` (generation,partition,height,block_hash,reason,payload) VALUES ($1,$2,$3,$4,'dual_node_or_inventory_parity',$5) ON CONFLICT DO NOTHING`, a.generation, partition, end, endHash, payload); err != nil {
			return snapshot, err
		}
		return snapshot, ErrParity
	}
	endHeader, err := a.rpc.HeaderByHash(bounded, common.HexToHash(endHash))
	if err != nil {
		return snapshot, err
	}
	if endHeader == nil || endHeader.Time > 1<<63-1 || creation.Time > 1<<63-1 {
		return snapshot, delivery.ErrIntegrity
	}
	snapshot.ObservedThrough = time.Unix(int64(endHeader.Time), 0).UTC()
	legs := make([]TimedLeg, len(records))
	for i := range records {
		legs[i] = records[i].TimedLeg
	}
	day := time.Unix(int64(creation.Time), 0).UTC()
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Unix(int64(endHeader.Time), 0).UTC()
	endDay = time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 0, 0, 0, 0, time.UTC)
	if endDay.Before(day) || endDay.Sub(day) > 3660*24*time.Hour {
		return snapshot, ErrBaselineUnavailable
	}
	var previous Checkpoint
	for !day.After(endDay) {
		var cp Checkpoint
		daily := legs
		if paged {
			daily = make([]TimedLeg, 0)
			next := day.AddDate(0, 0, 1)
			for _, entry := range legs {
				if !entry.CommittedAt.Before(day) && entry.CommittedAt.Before(next) {
					daily = append(daily, entry)
				}
			}
		}
		if paged {
			cp, err = RebuildDayPages(bounded, previous, daily, day)
		} else if len(snapshot.Checkpoints) == 0 {
			cp, err = RebuildDay(bounded, Empty(), daily, day, true)
		} else {
			cp, err = ContinueDay(bounded, previous, daily, day)
		}
		if err != nil {
			return snapshot, err
		}
		snapshot.Checkpoints = append(snapshot.Checkpoints, cp)
		previous = cp
		day = day.AddDate(0, 0, 1)
	}
	if previous.State != state {
		return snapshot, delivery.ErrIntegrity
	}
	input, _ := json.Marshal(records)
	snapshot.InputDigest = delivery.Hash(input)
	finalOwner, finalEpoch, err := a.sourceLease(bounded, tx)
	if err != nil {
		return snapshot, err
	}
	if finalOwner != owner || finalEpoch != epoch {
		return snapshot, delivery.ErrLease
	}
	if err = s.livePersistReplayTx(bounded, tx, snapshot, records); err != nil {
		return snapshot, err
	}
	if err = a.epochGuard(bounded, tx); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (a *BaselineAuthority) liveMovementPages(ctx context.Context, tx pgx.Tx, token string, start, end int64) ([]movement, string, string, error) {
	all := []movement{}
	proofs := []string{}
	lastHash := ""
	for from := start; from <= end; {
		to := from + 9999
		if to > end {
			to = end
		}
		blocks, e := a.coverage(ctx, tx, from, to)
		if e != nil {
			return nil, "", "", e
		}
		chain, e := a.rpc.ChainID(ctx)
		if e != nil {
			return nil, "", "", e
		}
		if chain.Cmp(big.NewInt(125)) != 0 {
			return nil, "", "", delivery.ErrIntegrity
		}
		logs, e := a.liveTransferLogs(ctx, token, from, to)
		if e != nil {
			return nil, "", "", e
		}
		sort.Slice(logs, func(i, j int) bool {
			x, y := logs[i], logs[j]
			if x.BlockNumber != y.BlockNumber {
				return x.BlockNumber < y.BlockNumber
			}
			if x.TxIndex != y.TxIndex {
				return x.TxIndex < y.TxIndex
			}
			return x.Index < y.Index
		})
		rpcMoves := make([]movement, len(logs))
		for i, log := range logs {
			m, e := decodeTransfer(log)
			if e != nil || strings.ToLower(log.Address.Hex()) != token || blocks[m.Height] != m.BlockHash {
				return nil, "", "", delivery.ErrIntegrity
			}
			if i > 0 && m.Height == rpcMoves[i-1].Height && m.LogIndex <= rpcMoves[i-1].LogIndex {
				return nil, "", "", delivery.ErrIntegrity
			}
			rpcMoves[i] = m
		}
		cursorHeight, cursorTx, cursorLog := from-1, int64(-1), int64(-1)
		offset := 0
		for {
			rows, e := tx.Query(ctx, `SELECT t.transaction_hash,t.log_index,t.height,t.transaction_index,t.block_hash,t.from_address,t.to_address,t.amount_raw,r.emitter,r.topics,r.data FROM indexer.indexer_token_transfers_v1 t JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=t.chain_id AND r.transaction_hash=t.transaction_hash AND r.log_index=t.log_index WHERE t.chain_id=125 AND t.token=$1 AND t.height BETWEEN $2 AND $3 AND (t.height,t.transaction_index,t.log_index)>($4,$5,$6) ORDER BY t.height,t.transaction_index,t.log_index LIMIT 10000`, token, from, to, cursorHeight, cursorTx, cursorLog)
			if e != nil {
				return nil, "", "", e
			}
			count := 0
			for rows.Next() {
				var m movement
				var logIndex, txIndex int64
				var emitter string
				var topicRaw, data []byte
				if e = rows.Scan(&m.TransactionHash, &logIndex, &m.Height, &txIndex, &m.BlockHash, &m.From, &m.To, &m.Amount, &emitter, &topicRaw, &data); e != nil {
					rows.Close()
					return nil, "", "", e
				}
				if logIndex < 0 || logIndex > 1<<32-1 || txIndex < 0 || txIndex > 1<<32-1 || emitter != token {
					rows.Close()
					return nil, "", "", delivery.ErrIntegrity
				}
				m.LogIndex = uint32(logIndex)
				m.TransactionIndex = uint32(txIndex)
				var topics []string
				if e = json.Unmarshal(topicRaw, &topics); e != nil {
					rows.Close()
					return nil, "", "", e
				}
				if len(topics) != 3 {
					rows.Close()
					return nil, "", "", delivery.ErrIntegrity
				}
				log := types.Log{Address: common.HexToAddress(emitter), Topics: []common.Hash{common.HexToHash(topics[0]), common.HexToHash(topics[1]), common.HexToHash(topics[2])}, Data: data, BlockNumber: uint64(m.Height), TxHash: common.HexToHash(m.TransactionHash), TxIndex: uint(m.TransactionIndex), BlockHash: common.HexToHash(m.BlockHash), Index: uint(m.LogIndex)}
				decoded, de := decodeTransfer(log)
				if de != nil || decoded != m || offset >= len(rpcMoves) || rpcMoves[offset] != m {
					rows.Close()
					return nil, "", "", delivery.ErrIntegrity
				}
				offset++
				count++
				cursorHeight, cursorTx, cursorLog = m.Height, txIndex, logIndex
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, "", "", e
			}
			if count < 10000 {
				break
			}
		}
		if offset != len(rpcMoves) {
			return nil, "", "", delivery.ErrGap
		}
		if len(all)+len(rpcMoves) > 1000000 {
			return nil, "", "", ErrBaselineUnavailable
		}
		_, reportedTo, e := reportedHeaderByNumber(ctx, a.rpc, big.NewInt(to))
		if e != nil {
			return nil, "", "", e
		}
		if reportedTo != blocks[to] {
			return nil, "", "", delivery.ErrIntegrity
		}
		raw, e := json.Marshal(rpcMoves)
		if e != nil {
			return nil, "", "", e
		}
		proofs = append(proofs, delivery.Hash(raw))
		all = append(all, rpcMoves...)
		lastHash = blocks[to]
		if to == end {
			break
		}
		from = to + 1
	}
	raw, e := json.Marshal(proofs)
	if e != nil {
		return nil, "", "", e
	}
	return all, delivery.Hash(raw), lastHash, nil
}

func (a *BaselineAuthority) liveTransferLogs(ctx context.Context, token string, from, to int64) ([]types.Log, error) {
	logs, e := a.rpc.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: big.NewInt(from), ToBlock: big.NewInt(to), Addresses: []common.Address{common.HexToAddress(token)}, Topics: [][]common.Hash{{transferTopic}}})
	if e == nil && len(logs) <= 10000 {
		return logs, nil
	}
	if from < to {
		middle := from + (to-from)/2
		left, e := a.liveTransferLogs(ctx, token, from, middle)
		if e != nil {
			return nil, e
		}
		right, e := a.liveTransferLogs(ctx, token, middle+1, to)
		if e != nil {
			return nil, e
		}
		if len(left)+len(right) > 1000000 {
			return nil, ErrBaselineUnavailable
		}
		return append(left, right...), nil
	}
	block, e := reportedBlockByNumber(ctx, a.rpc, big.NewInt(from))
	if e != nil {
		return nil, e
	}
	result := []types.Log{}
	for index, transaction := range block.Transactions {
		receipt, e := a.rpc.TransactionReceipt(ctx, transaction)
		if e != nil {
			return nil, e
		}
		if receipt == nil || receipt.TxHash != transaction || receipt.BlockHash != block.Hash || receipt.BlockNumber == nil || receipt.BlockNumber.Int64() != from || int(receipt.TransactionIndex) != index {
			return nil, delivery.ErrIntegrity
		}
		for _, log := range receipt.Logs {
			if log == nil {
				return nil, delivery.ErrIntegrity
			}
			if strings.ToLower(log.Address.Hex()) == token && len(log.Topics) > 0 && log.Topics[0] == transferTopic {
				if log.TxHash != receipt.TxHash || log.BlockHash != block.Hash || int(log.TxIndex) != index {
					return nil, delivery.ErrIntegrity
				}
				result = append(result, *log)
				if len(result) > 1000000 {
					return nil, ErrBaselineUnavailable
				}
			}
		}
	}
	return result, nil
}

func (a *BaselineAuthority) liveReceiptSettlement(ctx context.Context, tx pgx.Tx, fill accounting.FillV1) (receiptSettlement, error) {
	var result receiptSettlement
	if e := accounting.ValidateFillV1(fill); e != nil {
		return result, e
	}
	receipt, e := a.rpc.TransactionReceipt(ctx, common.HexToHash(fill.Position.TransactionHash))
	if e != nil {
		return result, e
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.String() != fill.Position.Height || strings.ToLower(receipt.BlockHash.Hex()) != fill.Position.BlockHash || strings.ToLower(receipt.TxHash.Hex()) != fill.Position.TransactionHash || strconv.FormatUint(uint64(receipt.TransactionIndex), 10) != fill.Position.TransactionIndex {
		return result, delivery.ErrIntegrity
	}
	raw, e := json.Marshal(receipt)
	if e != nil {
		return result, e
	}
	if delivery.Hash(raw) != fill.Provenance.ReceiptSHA256 {
		return result, delivery.ErrIntegrity
	}
	header, e := a.rpc.HeaderByHash(ctx, receipt.BlockHash)
	if e != nil {
		return result, e
	}
	if header == nil || strconv.FormatUint(header.Time, 10) != fill.BlockTimestampSeconds {
		return result, delivery.ErrIntegrity
	}
	raw, e = json.Marshal(header)
	if e != nil {
		return result, e
	}
	if delivery.Hash(raw) != fill.Provenance.HeaderSHA256 {
		return result, delivery.ErrIntegrity
	}
	rows, e := tx.Query(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE payload->'position'->>'transactionHash'=$1 ORDER BY (payload->'position'->>'logIndex')::bigint LIMIT 10001`, fill.Position.TransactionHash)
	if e != nil {
		return result, e
	}
	fills := []accounting.FillV1{}
	for rows.Next() {
		var raw []byte
		var digest string
		if e = rows.Scan(&raw, &digest); e != nil {
			rows.Close()
			return result, e
		}
		f, de := accounting.DecodeFillV1(raw)
		if de != nil {
			rows.Close()
			return result, de
		}
		actual, de := accounting.FillDigest(f)
		if de != nil || string(actual) != digest || f.Position.ChainID != fill.Position.ChainID || f.Position.Height != fill.Position.Height || f.Position.TransactionHash != fill.Position.TransactionHash || f.Position.BlockHash != fill.Position.BlockHash || f.Position.TransactionIndex != fill.Position.TransactionIndex || f.Provenance.ReceiptSHA256 != fill.Provenance.ReceiptSHA256 || f.Provenance.HeaderSHA256 != fill.Provenance.HeaderSHA256 {
			rows.Close()
			return result, delivery.ErrIntegrity
		}
		fills = append(fills, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	if len(fills) == 0 || len(fills) > 10000 {
		return result, ErrCostAllocationUnavailable
	}
	used := map[uint32]bool{}
	type settlementAccount struct{ asset, wallet string }
	expectedNet := map[settlementAccount]*big.Int{}
	found := false
	for _, f := range fills {
		end, e := strconv.ParseUint(f.Position.LogIndex, 10, 32)
		if e != nil {
			return result, e
		}
		var tokenCandidates, quoteCandidates []movement
		for _, log := range receipt.Logs {
			if log == nil {
				return result, delivery.ErrIntegrity
			}
			if uint64(log.Index) >= end || used[uint32(log.Index)] || len(log.Topics) == 0 || log.Topics[0] != transferTopic {
				continue
			}
			emitter := strings.ToLower(log.Address.Hex())
			if emitter != f.Token.Address && emitter != f.Settlement.Address {
				continue
			}
			m, e := decodeTransfer(*log)
			if e != nil {
				return result, e
			}
			if m.TransactionHash != f.Position.TransactionHash || m.BlockHash != f.Position.BlockHash || strconv.FormatInt(m.Height, 10) != f.Position.Height || strconv.FormatUint(uint64(m.TransactionIndex), 10) != f.Position.TransactionIndex {
				return result, delivery.ErrIntegrity
			}
			if emitter == f.Token.Address && m.Amount == f.TokenAmountRaw && (f.Side == "buy" && m.From == f.PoolAddress || f.Side == "sell" && m.To == f.PoolAddress) {
				tokenCandidates = append(tokenCandidates, m)
			}
			if emitter == f.Settlement.Address && m.Amount == f.USDLAmountRaw && (f.Side == "buy" && m.To == f.PoolAddress || f.Side == "sell" && m.From == f.PoolAddress) {
				quoteCandidates = append(quoteCandidates, m)
			}
		}
		if len(tokenCandidates) != 1 || len(quoteCandidates) != 1 {
			return result, ErrCostAllocationUnavailable
		}
		token, quote := tokenCandidates[0], quoteCandidates[0]
		used[token.LogIndex] = true
		used[quote.LogIndex] = true
		payer, recipient := quote.From, token.To
		if f.Side == "sell" {
			payer, recipient = token.From, quote.To
		}
		if payer == zeroAddress || recipient == zeroAddress {
			return result, ErrCostAllocationUnavailable
		}
		if payer == recipient {
			account := settlementAccount{f.Settlement.Address, payer}
			if expectedNet[account] == nil {
				expectedNet[account] = new(big.Int)
			}
			amount, parseErr := Uint256(f.USDLAmountRaw)
			if parseErr != nil {
				return result, parseErr
			}
			if f.Side == "buy" {
				expectedNet[account].Sub(expectedNet[account], amount)
			} else {
				expectedNet[account].Add(expectedNet[account], amount)
			}
		}
		if f.SourceFillID == fill.SourceFillID {
			if found {
				return result, delivery.ErrIntegrity
			}
			found = true
			if receipt.EffectiveGasPrice == nil || receipt.EffectiveGasPrice.Sign() < 0 {
				return result, delivery.ErrIntegrity
			}
			result = receiptSettlement{fill: f, tokenMovement: token, payer: payer, recipient: recipient, gas: new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice).String(), digest: f.Provenance.ReceiptSHA256}
		}
	}
	if !found {
		return result, delivery.ErrGap
	}
	actualNet := map[settlementAccount]*big.Int{}
	settlementAssets := map[string]bool{}
	for account := range expectedNet {
		actualNet[account] = new(big.Int)
		settlementAssets[account.asset] = true
	}
	for _, log := range receipt.Logs {
		if log == nil {
			return result, delivery.ErrIntegrity
		}
		if len(log.Topics) == 0 || log.Topics[0] != transferTopic {
			continue
		}
		asset := strings.ToLower(log.Address.Hex())
		if !settlementAssets[asset] {
			continue
		}
		m, decodeErr := decodeTransfer(*log)
		if decodeErr != nil || m.TransactionHash != fill.Position.TransactionHash || m.BlockHash != fill.Position.BlockHash || strconv.FormatInt(m.Height, 10) != fill.Position.Height || strconv.FormatUint(uint64(m.TransactionIndex), 10) != fill.Position.TransactionIndex {
			return result, delivery.ErrIntegrity
		}
		amount, parseErr := Uint256(m.Amount)
		if parseErr != nil {
			return result, parseErr
		}
		if balance := actualNet[settlementAccount{asset, m.To}]; balance != nil {
			balance.Add(balance, amount)
		}
		if balance := actualNet[settlementAccount{asset, m.From}]; balance != nil {
			balance.Sub(balance, amount)
		}
	}
	for account, expected := range expectedNet {
		if expected.Cmp(actualNet[account]) != 0 {
			return result, ErrCostAllocationUnavailable
		}
	}
	return result, nil
}

const LiveTransfersDDL = `CREATE TABLE inventory_l16_live_owners_v1 (
 wallet text PRIMARY KEY CHECK(wallet ~ '^0x[0-9a-f]{40}$'),account_id text NOT NULL,session_id text NOT NULL,auth_epoch bigint NOT NULL CHECK(auth_epoch>=0),proof_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 accepted_height bigint NOT NULL DEFAULT 0 CHECK(accepted_height>=0),accepted_hash text,accepted_snapshot uuid REFERENCES portfolio_snapshots(id),source_digest text,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),last_attempt_at timestamptz NOT NULL DEFAULT 'epoch',cancelled_at timestamptz);
CREATE INDEX inventory_l16_live_owners_due_v1 ON inventory_l16_live_owners_v1(last_attempt_at,wallet) WHERE cancelled_at IS NULL;
CREATE TABLE inventory_l16_live_commits_v1 (
 generation text NOT NULL,wallet text NOT NULL,source_stream text NOT NULL,height bigint NOT NULL CHECK(height>0),block_hash text NOT NULL,source_owner text NOT NULL,source_epoch bigint NOT NULL CHECK(source_epoch>0),release_digest text NOT NULL,input_digest text NOT NULL,snapshot_id uuid NOT NULL REFERENCES portfolio_snapshots(id),
 PRIMARY KEY(generation,wallet,source_stream,height),UNIQUE(snapshot_id));
CREATE TRIGGER inventory_l16_live_commit_immutable_v1 BEFORE UPDATE OR DELETE ON inventory_l16_live_commits_v1 FOR EACH ROW EXECUTE FUNCTION inventory_l16_append_only_v2();`

type LiveIdentityAuthority interface {
	auth.RequestIdentityAuthority
	CheckStreamSession(context.Context, auth.StreamPrincipal) error
}

type LiveTransferWorker struct {
	service    *Service
	repository l17.Repository
	identity   LiveIdentityAuthority
	batch      int
}

func NewLiveTransferWorker(service *Service, repository l17.Repository, identity LiveIdentityAuthority, batch int) (*LiveTransferWorker, error) {
	if service == nil || service.authority == nil || service.authority.epochGuard == nil || repository.Pool != service.authority.pool || identity == nil || batch < 1 || batch > 32 {
		return nil, ErrBaselineUnavailable
	}
	return &LiveTransferWorker{service: service, repository: repository, identity: identity, batch: batch}, nil
}

func (w *LiveTransferWorker) EnqueueOwned(ctx context.Context, p auth.IdentityPrincipal) error {
	now := time.Now().UTC()
	if e := p.Validate(now); e != nil {
		return e
	}
	if p.ProofAt.Before(now.Add(-15 * time.Minute)) {
		return auth.ErrScopeDenied
	}
	if e := w.identity.CheckStreamSession(ctx, p.StreamPrincipal()); e != nil {
		return e
	}
	a := w.service.authority
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer inventoryRollback(tx)
	if e = a.epochGuard(ctx, tx); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_live_owners_v1")+` (wallet,account_id,session_id,auth_epoch,proof_at,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(wallet) DO UPDATE SET account_id=EXCLUDED.account_id,session_id=EXCLUDED.session_id,auth_epoch=EXCLUDED.auth_epoch,proof_at=EXCLUDED.proof_at,expires_at=EXCLUDED.expires_at,cancelled_at=NULL,updated_at=clock_timestamp()`, p.Wallet, p.AccountID, p.SessionID, p.AuthEpoch, p.ProofAt, p.AccessExpiresAt)
	if e != nil {
		return e
	}
	if e = w.identity.CheckStreamSession(ctx, p.StreamPrincipal()); e != nil {
		return e
	}
	if e = a.epochGuard(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (w *LiveTransferWorker) EnrollmentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal, e := w.identity.AuthenticateRequest(request.Context(), request)
		if e != nil {
			http.Error(response, "owner identity required", http.StatusUnauthorized)
			return
		}
		if e = w.EnqueueOwned(request.Context(), principal); e != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(e, auth.ErrScopeDenied) {
				status = http.StatusForbidden
			}
			http.Error(response, "portfolio enrollment unavailable", status)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (w *LiveTransferWorker) CancelOwned(ctx context.Context, p auth.IdentityPrincipal) error {
	if e := p.Validate(time.Now()); e != nil {
		return e
	}
	if e := w.identity.CheckStreamSession(ctx, p.StreamPrincipal()); e != nil {
		return e
	}
	a := w.service.authority
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer inventoryRollback(tx)
	if e = a.epochGuard(ctx, tx); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE `+a.table("inventory_l16_live_owners_v1")+` SET cancelled_at=clock_timestamp() WHERE wallet=$1 AND account_id=$2`, p.Wallet, p.AccountID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return auth.ErrScopeDenied
	}
	if e = a.epochGuard(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (w *LiveTransferWorker) RunOnce(ctx context.Context) (int, error) {
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ctx = bounded
	count := 0
	for count < w.batch {
		changed, e := w.processNext(ctx)
		if e != nil {
			return count, e
		}
		if !changed {
			return count, nil
		}
		count++
	}
	return count, nil
}

func (w *LiveTransferWorker) processNext(ctx context.Context) (bool, error) {
	return w.processWallet(ctx, "")
}

func (w *LiveTransferWorker) processWallet(ctx context.Context, selectedWallet string) (bool, error) {
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a := w.service.authority
	tx, e := a.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return false, e
	}
	defer inventoryRollback(tx)
	if e = a.epochGuard(bounded, tx); e != nil {
		return false, e
	}
	if _, e = tx.Exec(bounded, `SET LOCAL search_path TO `+a.schema+`,pg_catalog`); e != nil {
		return false, e
	}
	var p auth.IdentityPrincipal
	var priorHeight int64
	var priorHash *string
	e = tx.QueryRow(bounded, `SELECT wallet,account_id,session_id,auth_epoch,proof_at,expires_at,accepted_height,accepted_hash FROM inventory_l16_live_owners_v1 WHERE cancelled_at IS NULL AND expires_at>clock_timestamp() AND proof_at>clock_timestamp()-interval '15 minutes' AND ($1='' AND last_attempt_at<clock_timestamp()-interval '1 second' OR $1=wallet) ORDER BY last_attempt_at,wallet LIMIT 1 FOR UPDATE SKIP LOCKED`, selectedWallet).Scan(&p.Wallet, &p.AccountID, &p.SessionID, &p.AuthEpoch, &p.ProofAt, &p.AccessExpiresAt, &priorHeight, &priorHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if e = p.Validate(time.Now()); e != nil {
		return false, e
	}
	if e = w.identity.CheckStreamSession(bounded, p.StreamPrincipal()); e != nil {
		return false, e
	}
	if _, e = tx.Exec(bounded, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portfolio:"+a.generation+":"+p.Wallet); e != nil {
		return false, e
	}
	head, headHash, e := w.acceptedHeadTx(bounded, tx)
	if e != nil {
		return false, e
	}
	if head < priorHeight || head == priorHeight && (priorHash == nil || *priorHash != headHash) {
		return false, delivery.ErrIntegrity
	}
	if head == priorHeight {
		if _, e = tx.Exec(bounded, `UPDATE inventory_l16_live_owners_v1 SET last_attempt_at=clock_timestamp() WHERE wallet=$1`, p.Wallet); e != nil {
			return false, e
		}
		return true, tx.Commit(bounded)
	}
	var quarantined bool
	if e = tx.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM inventory_l16_quarantine_v2 WHERE generation=$1 AND left(partition,length($2))=$2 AND height<=$3)`, a.generation, "125:"+p.Wallet+":", head).Scan(&quarantined); e != nil {
		return false, e
	}
	if quarantined {
		return false, ErrParity
	}
	owner, epoch, e := a.sourceLease(bounded, tx)
	if e != nil {
		return false, e
	}
	if _, e = tx.Exec(bounded, `SAVEPOINT live_wallet_fold`); e != nil {
		return false, e
	}
	snapshots := []RebuildSnapshot{}
	after := ""
	for {
		rows, e := tx.Query(bounded, `SELECT DISTINCT t.token FROM indexer.indexer_token_transfers_v1 t JOIN indexer.indexer_canonical_creations_v1 c ON c.token=t.token WHERE t.chain_id=125 AND t.height<=$1 AND (t.from_address=$2 OR t.to_address=$2) AND t.token>$3 ORDER BY t.token LIMIT 10000`, head, p.Wallet, after)
		if e != nil {
			return false, e
		}
		tokens := []string{}
		for rows.Next() {
			var token string
			if e = rows.Scan(&token); e != nil {
				rows.Close()
				return false, e
			}
			tokens = append(tokens, token)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return false, e
		}
		for _, token := range tokens {
			snapshot, e := w.service.liveRebuildTx(bounded, tx, p.Wallet, token, head)
			if errors.Is(e, ErrParity) {
				if _, e = tx.Exec(bounded, `ROLLBACK TO SAVEPOINT live_wallet_fold`); e != nil {
					return false, e
				}
				raw, e := json.Marshal(snapshot)
				if e != nil {
					return false, e
				}
				if _, e = tx.Exec(bounded, `INSERT INTO inventory_l16_quarantine_v2(generation,partition,height,block_hash,reason,payload) VALUES($1,$2,$3,$4,'dual_node_or_inventory_parity',$5) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, head, snapshot.BlockHash, raw); e != nil {
					return false, e
				}
				if _, e = tx.Exec(bounded, `UPDATE inventory_l16_live_owners_v1 SET last_attempt_at=clock_timestamp() WHERE wallet=$1`, p.Wallet); e != nil {
					return false, e
				}
				if e = a.epochGuard(bounded, tx); e != nil {
					return false, e
				}
				if e = tx.Commit(bounded); e != nil {
					return false, e
				}
				return false, ErrParity
			}
			if e != nil {
				return false, e
			}
			if snapshot.BlockHash != headHash || snapshot.SourceOwner != owner || snapshot.SourceEpoch != epoch {
				return false, delivery.ErrIntegrity
			}
			snapshots = append(snapshots, snapshot)
			if len(snapshots) > 100000 {
				return false, ErrBaselineUnavailable
			}
			after = token
		}
		if len(tokens) < 10000 {
			break
		}
	}
	summary, source, e := w.commitWalletTx(bounded, tx, p.Wallet, head, headHash, snapshots)
	if e != nil {
		return false, e
	}
	if e = w.identity.CheckStreamSession(bounded, p.StreamPrincipal()); e != nil {
		return false, e
	}
	if e = p.Validate(time.Now()); e != nil {
		return false, e
	}
	if p.ProofAt.Before(time.Now().Add(-15 * time.Minute)) {
		return false, auth.ErrScopeDenied
	}
	var accepted bool
	if e = tx.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_committed_heads_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND block_hash=$3 AND integrity='healthy')`, a.sourceStream, head, headHash).Scan(&accepted); e != nil {
		return false, e
	}
	if !accepted {
		return false, delivery.ErrIntegrity
	}
	finalOwner, finalEpoch, e := a.sourceLease(bounded, tx)
	if e != nil {
		return false, e
	}
	if finalOwner != owner || finalEpoch != epoch {
		return false, delivery.ErrLease
	}
	if _, e = tx.Exec(bounded, `INSERT INTO inventory_l16_live_commits_v1 (generation,wallet,source_stream,height,block_hash,source_owner,source_epoch,release_digest,input_digest,snapshot_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.generation, p.Wallet, a.sourceStream, head, headHash, owner, epoch, w.service.release, source, summary.SnapshotID); e != nil {
		return false, e
	}
	tag, e := tx.Exec(bounded, `UPDATE inventory_l16_live_owners_v1 SET accepted_height=$2,accepted_hash=$3,accepted_snapshot=$4,source_digest=$5,last_attempt_at=clock_timestamp() WHERE wallet=$1 AND account_id=$6 AND session_id=$7 AND cancelled_at IS NULL AND expires_at>clock_timestamp()`, p.Wallet, head, headHash, summary.SnapshotID, source, p.AccountID, p.SessionID)
	if e != nil {
		return false, e
	}
	if tag.RowsAffected() != 1 {
		return false, auth.ErrScopeDenied
	}
	if e = a.epochGuard(bounded, tx); e != nil {
		return false, e
	}
	return true, tx.Commit(bounded)
}

func (w *LiveTransferWorker) acceptedHeadTx(ctx context.Context, tx pgx.Tx) (int64, string, error) {
	var height int64
	var blockHash string
	e := tx.QueryRow(ctx, `SELECT c.height,c.block_hash FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=w.chain_id AND c.stream_id=w.stream_id AND c.height=LEAST(w.raw_complete_height,w.typed_complete_height) JOIN indexer.indexer_committed_heads_v1 h ON h.chain_id=c.chain_id AND h.stream_id=c.stream_id AND h.height=c.height AND h.block_hash=c.block_hash AND h.integrity='healthy' WHERE w.chain_id=125 AND w.stream_id=$1 AND c.fetch_complete AND c.required_decode_complete AND c.raw_count=(SELECT count(*) FROM indexer.indexer_raw_logs_v1 r WHERE r.chain_id=c.chain_id AND r.height=c.height)`, w.service.authority.sourceStream).Scan(&height, &blockHash)
	if e != nil {
		return 0, "", e
	}
	if height <= 0 || !hash.MatchString(blockHash) {
		return 0, "", delivery.ErrIntegrity
	}
	return height, blockHash, nil
}

func (w *LiveTransferWorker) commitWalletTx(ctx context.Context, tx pgx.Tx, wallet string, head int64, headHash string, snapshots []RebuildSnapshot) (l17.Summary, string, error) {
	var summary l17.Summary
	raw, e := json.Marshal(struct {
		Generation, Release, Wallet, Hash string
		Height                            int64
		Snapshots                         []RebuildSnapshot
	}{w.service.authority.generation, w.service.release, wallet, headHash, head, snapshots})
	if e != nil {
		return summary, "", e
	}
	source := delivery.Hash(raw)
	var revisionRaw string
	if e = tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0)::text FROM portfolio_snapshots WHERE wallet=$1`, wallet).Scan(&revisionRaw); e != nil {
		return summary, "", e
	}
	revision, e := Uint256(revisionRaw)
	if e != nil {
		return summary, "", e
	}
	revision.Add(revision, big.NewInt(1))
	if revision.BitLen() > 256 {
		return summary, "", delivery.ErrIntegrity
	}
	header, e := w.service.authority.rpc.HeaderByHash(ctx, common.HexToHash(headHash))
	if e != nil {
		return summary, "", e
	}
	if header == nil || header.Number == nil || header.Number.Int64() != head || header.Time > 1<<63-1 {
		return summary, "", delivery.ErrIntegrity
	}
	at := time.Unix(int64(header.Time), 0).UTC()
	watermark := l17.Watermark{ChainID: "125", Height: strconv.FormatInt(head, 10), BlockHash: headHash, ProjectionRevision: revision.String(), ObservedAt: at.Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	positions := make([]l17.Position, 0, len(snapshots))
	for _, snapshot := range snapshots {
		parts := strings.Split(snapshot.Partition, ":")
		if len(parts) != 3 || parts[1] != wallet || snapshot.Height != head || snapshot.BlockHash != headHash || !snapshot.ObservedThrough.Equal(at) {
			return summary, "", delivery.ErrIntegrity
		}
		mark, e := w.service.authority.markPriceTx(ctx, tx, parts[2], head, headHash)
		if e != nil {
			return summary, "", e
		}
		quality := "unpriced"
		if mark != nil {
			quality = "priced"
		}
		position, e := ProjectPosition(wallet, parts[2], snapshot.State, watermark, mark, quality)
		if e != nil {
			return summary, "", e
		}
		positions = append(positions, position)
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("live-transfers:"+source)).String()
	summary, e = l17.Aggregate(wallet, id, watermark, positions)
	if e != nil {
		return summary, "", e
	}
	notice := l17.Notice{Wallet: wallet, SnapshotID: id, Revision: revision.String(), Algorithm: Algorithm}
	payload, e := json.Marshal(notice)
	if e != nil {
		return summary, "", e
	}
	envelope := delivery.Envelope{EventID: source, EventType: "portfolio.snapshot.v2", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(head, 10), TransactionIndex: "0", LogIndex: "0"}, EmittedAt: at, TraceID: source, PayloadHash: delivery.Hash(payload), Payload: payload}
	summary, e = w.repository.CommitFullProjectionDeliveryTx(ctx, tx, w.service.outbox, "portfolio.snapshot.v2", source, summary, positions, time.Now().UTC().Add(24*time.Hour), envelope)
	if e != nil {
		return summary, "", e
	}
	if len(snapshots) > 0 {
		if e = w.service.commitRebuiltHistoryTx(ctx, tx, wallet, id, source, snapshots, w.repository); e != nil {
			return summary, "", e
		}
	} else {
		if e = w.repository.CommitHistoryTx(ctx, tx, wallet, id, source, []l17.HistoryRecord{}); e != nil {
			return summary, "", e
		}
	}
	return summary, source, nil
}

func (s *Service) livePersistReplayTx(ctx context.Context, tx pgx.Tx, snapshot RebuildSnapshot, records []EconomicRecord) error {
	a := s.authority
	for _, record := range records {
		identity, err := record.Leg.Identity.Key()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		digest := delivery.Hash(raw)
		if _, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_archive_v2")+` (generation,partition,identity,payload_hash,payload) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, identity, digest, raw); err != nil {
			return err
		}
		var old string
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM `+a.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2 AND identity=$3`, a.generation, snapshot.Partition, identity).Scan(&old); err != nil {
			return err
		}
		if old != digest {
			return delivery.ErrIntegrity
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	digest := delivery.Hash(raw)
	if _, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_snapshots_v2")+` (generation,partition,snapshot_digest,payload,height,block_hash,release_digest) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, digest, raw, snapshot.Height, snapshot.BlockHash, snapshot.ReleaseDigest); err != nil {
		return err
	}
	for _, cp := range snapshot.Checkpoints {
		payload, e := json.Marshal(struct {
			Checkpoint    Checkpoint `json:"checkpoint"`
			ReleaseDigest string     `json:"releaseDigest"`
			InputDigest   string     `json:"inputDigest"`
		}{cp, snapshot.ReleaseDigest, snapshot.InputDigest})
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_checkpoints_v2")+` (generation,partition,day,snapshot_digest,checkpoint_digest,payload) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, a.generation, snapshot.Partition, cp.Day, digest, delivery.Hash(payload), payload); e != nil {
			return e
		}
	}
	var currentHeight int64
	var currentGeneration, currentDigest string
	err = tx.QueryRow(ctx, `SELECT height,generation,snapshot_digest FROM `+a.table("inventory_l16_active_v2")+` WHERE partition=$1 FOR UPDATE`, snapshot.Partition).Scan(&currentHeight, &currentGeneration, &currentDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && currentHeight > snapshot.Height {
		return delivery.ErrIntegrity
	}
	if err == nil && currentHeight == snapshot.Height && currentDigest != digest {
		var priorRaw []byte
		if e := tx.QueryRow(ctx, `SELECT payload FROM `+a.table("inventory_l16_snapshots_v2")+` WHERE generation=$1 AND partition=$2 AND snapshot_digest=$3`, currentGeneration, snapshot.Partition, currentDigest).Scan(&priorRaw); e != nil {
			return e
		}
		var prior RebuildSnapshot
		if delivery.Hash(priorRaw) != currentDigest {
			return delivery.ErrIntegrity
		}
		if e := json.Unmarshal(priorRaw, &prior); e != nil {
			return e
		}
		if currentGeneration != a.generation || !sameReplaySnapshot(prior, snapshot) {
			return delivery.ErrIntegrity
		}
	}
	now := time.Now().UTC()
	envelope := delivery.Envelope{EventID: digest, EventType: RebuildSubject, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(snapshot.Height, 10), TransactionIndex: "0", LogIndex: "0"}, EmittedAt: now, TraceID: digest, PayloadHash: digest, Payload: raw}
	if err == nil && currentDigest == digest {
		return nil
	}
	if err = s.outbox.Enqueue(ctx, tx, RebuildSubject, envelope); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+a.table("inventory_l16_active_v2")+` (partition,generation,snapshot_digest,height) VALUES ($1,$2,$3,$4) ON CONFLICT (partition) DO UPDATE SET generation=EXCLUDED.generation,snapshot_digest=EXCLUDED.snapshot_digest,height=EXCLUDED.height`, snapshot.Partition, a.generation, digest, snapshot.Height)
	return err
}

func (w *LiveTransferWorker) RebuildOwned(ctx context.Context, p auth.IdentityPrincipal) (l17.Summary, error) {
	if e := w.EnqueueOwned(ctx, p); e != nil {
		return l17.Summary{}, e
	}
	if _, e := w.processWallet(ctx, p.Wallet); e != nil {
		return l17.Summary{}, e
	}
	var raw []byte
	e := w.service.authority.pool.QueryRow(ctx, `SELECT s.summary FROM `+w.service.authority.table("inventory_l16_live_owners_v1")+` o JOIN `+w.service.authority.table("portfolio_snapshots")+` s ON s.id=o.accepted_snapshot WHERE o.wallet=$1 AND o.account_id=$2 AND o.cancelled_at IS NULL AND o.expires_at>clock_timestamp()`, p.Wallet, p.AccountID).Scan(&raw)
	if e != nil {
		return l17.Summary{}, e
	}
	var summary l17.Summary
	if e = json.Unmarshal(raw, &summary); e != nil {
		return summary, e
	}
	if e = w.identity.CheckStreamSession(ctx, p.StreamPrincipal()); e != nil {
		return l17.Summary{}, e
	}
	return summary, nil
}
