package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
)

type TokenCreationMetadata struct {
	Emitter     string                    `json:"emitter"`
	Name        string                    `json:"name"`
	Symbol      string                    `json:"symbol"`
	Decimals    uint8                     `json:"decimals"`
	TotalSupply string                    `json:"totalSupply"`
	Position    accounting.SourcePosition `json:"position"`
	Provenance  accounting.Provenance     `json:"provenance"`
}

type CreationFact struct {
	Metadata              *TokenCreationMetadata    `json:"metadata"`
	Token                 string                    `json:"token"`
	Pool                  string                    `json:"pool"`
	Creator               string                    `json:"creator"`
	Factory               string                    `json:"factory"`
	Optical               string                    `json:"optical"`
	NFTID                 string                    `json:"nftId"`
	Position              accounting.SourcePosition `json:"position"`
	BlockTimestampSeconds string                    `json:"blockTimestampSeconds"`
	Provenance            accounting.Provenance     `json:"provenance"`
}
type TransferFact struct {
	Token      string                    `json:"token"`
	From       string                    `json:"from"`
	To         string                    `json:"to"`
	AmountRaw  string                    `json:"amountRaw"`
	Position   accounting.SourcePosition `json:"position"`
	Provenance accounting.Provenance     `json:"provenance"`
}

func sourceProvenance(p accounting.Provenance) bool {
	return p.DeploymentID != "" && len(p.DeploymentID) <= 256 && p.ABIRevision != "" && len(p.ABIRevision) <= 256 && p.EmitterCodeEpoch != "" && len(p.EmitterCodeEpoch) <= 256 && len(p.ReceiptSHA256) == 64 && len(p.HeaderSHA256) == 64
}
func (c CanonicalTx) sourceBound(ctx context.Context, p accounting.SourcePosition, emitter string) error {
	if c.Tx == nil || accounting.ValidateSourcePosition(p) != nil {
		return ErrIntegrity
	}
	var bound bool
	err := c.Tx.QueryRow(ctx, `SELECT height=$4::bigint AND transaction_index=$5::bigint AND block_hash=$6 AND emitter=$7 FROM indexer.indexer_raw_logs WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2::bigint AND chain_id=$3`, p.TransactionHash, p.LogIndex, 125, p.Height, p.TransactionIndex, p.BlockHash, emitter).Scan(&bound)
	if err != nil {
		return err
	}
	if !bound {
		return ErrIntegrity
	}
	return nil
}
func (c CanonicalTx) InsertCreation(ctx context.Context, f CreationFact) error {
	zero := "0x" + strings.Repeat("0", 40)
	for _, a := range []string{f.Token, f.Pool, f.Creator, f.Factory} {
		if !addressPattern.MatchString(a) || a == zero {
			return ErrIntegrity
		}
	}
	if !addressPattern.MatchString(f.Optical) || !sourceProvenance(f.Provenance) || f.Provenance.PoolCodeEpoch == "" {
		return ErrIntegrity
	}
	if _, err := accounting.Uint(f.NFTID); err != nil {
		return err
	}
	ts, err := strconv.ParseInt(f.BlockTimestampSeconds, 10, 64)
	if err != nil || ts <= 0 {
		return ErrIntegrity
	}
	if err = c.sourceBound(ctx, f.Position, f.Factory); err != nil {
		return err
	}
	if f.Metadata != nil {
		m := f.Metadata
		if !addressPattern.MatchString(m.Emitter) || !sourceProvenance(m.Provenance) || len(m.Name) > 65536 || len(m.Symbol) > 65536 || m.Position.TransactionHash != f.Position.TransactionHash || m.Position.BlockHash != f.Position.BlockHash {
			return ErrIntegrity
		}
		if _, err = accounting.Uint(m.TotalSupply); err != nil {
			return err
		}
		if err = c.sourceBound(ctx, m.Position, m.Emitter); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	prov, err := json.Marshal(f.Provenance)
	if err != nil {
		return err
	}
	tag, err := c.Tx.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,$6::numeric,$7::bigint,$8,$9,$10::bigint,$11::bigint,$12,$13,$14) ON CONFLICT DO NOTHING`, f.Token, f.Pool, f.Creator, f.Factory, f.Optical, f.NFTID, f.Position.Height, f.Position.BlockHash, f.Position.TransactionHash, f.Position.TransactionIndex, f.Position.LogIndex, ts, prov, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = c.Tx.QueryRow(ctx, `SELECT payload=$2::jsonb FROM indexer.indexer_canonical_creations WHERE token=$1`, f.Token, raw).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return ErrIntegrity
		}
		return err
	}
	return nil
}
func (c CanonicalTx) HasCanonicalToken(ctx context.Context, token string) (bool, error) {
	var found bool
	err := c.Tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_canonical_creations WHERE token=$1)`, token).Scan(&found)
	return found, err
}
func (c CanonicalTx) InsertTokenTransfer(ctx context.Context, f TransferFact) error {
	if !addressPattern.MatchString(f.Token) || !addressPattern.MatchString(f.From) || !addressPattern.MatchString(f.To) || !sourceProvenance(f.Provenance) {
		return ErrIntegrity
	}
	if _, err := accounting.Uint(f.AmountRaw); err != nil {
		return err
	}
	if err := c.sourceBound(ctx, f.Position, f.Token); err != nil {
		return err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	prov, err := json.Marshal(f.Provenance)
	if err != nil {
		return err
	}
	tag, err := c.Tx.Exec(ctx, `INSERT INTO indexer.indexer_token_transfers(chain_id,transaction_hash,log_index,token,from_address,to_address,amount_raw,height,transaction_index,block_hash,provenance,payload) VALUES(125,$1,$2::bigint,$3,$4,$5,$6::numeric,$7::bigint,$8::bigint,$9,$10,$11) ON CONFLICT DO NOTHING`, f.Position.TransactionHash, f.Position.LogIndex, f.Token, f.From, f.To, f.AmountRaw, f.Position.Height, f.Position.TransactionIndex, f.Position.BlockHash, prov, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = c.Tx.QueryRow(ctx, `SELECT payload=$3::jsonb FROM indexer.indexer_token_transfers WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2::bigint`, f.Position.TransactionHash, f.Position.LogIndex, raw).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrIntegrity
		}
	}
	return nil
}
