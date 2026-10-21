package holdings

import (
	"errors"
	"strings"
)

type ActorSource string
type RecipientSource string

const (
	ActorUnknown      ActorSource     = "unknown"
	ActorChainEvent   ActorSource     = "authenticated_chain_event"
	RecipientTransfer RecipientSource = "erc20_transfer"
)

type Provenance struct {
	ChainID          string          `json:"chainId"`
	TransactionHash  string          `json:"transactionHash"`
	BlockHash        string          `json:"blockHash"`
	TransferIdentity string          `json:"transferIdentity"`
	Actor            *string         `json:"actor"`
	ActorSource      ActorSource     `json:"actorSource"`
	Recipient        string          `json:"recipient"`
	RecipientSource  RecipientSource `json:"recipientSource"`
}

type ChainActor struct {
	ChainID         string
	TransactionHash string
	BlockHash       string
	Address         string
}

func RecipientProvenance(input Transfer) (Provenance, error) {
	e, err := input.Normalize()
	if err != nil {
		return Provenance{}, err
	}
	return Provenance{ChainID: e.ChainID, TransactionHash: e.TransactionHash, BlockHash: e.BlockHash, TransferIdentity: e.Identity(), ActorSource: ActorUnknown, Recipient: e.To, RecipientSource: RecipientTransfer}, nil
}

func (p Provenance) WithActor(input ChainActor) (Provenance, error) {
	if input.ChainID != p.ChainID || !hexHash.MatchString(input.TransactionHash) || !hexHash.MatchString(input.BlockHash) || strings.ToLower(input.TransactionHash) != p.TransactionHash || strings.ToLower(input.BlockHash) != p.BlockHash {
		return p, ErrConflict
	}
	actor, err := Address(input.Address)
	if err != nil || actor == Zero {
		return p, errors.New("invalid chain actor")
	}
	p.Actor = &actor
	p.ActorSource = ActorChainEvent
	return p, nil
}

func ValidateProvenance(input Transfer, p Provenance) error {
	e, err := input.Normalize()
	if err != nil {
		return err
	}
	if p.ChainID != e.ChainID || p.TransactionHash != e.TransactionHash || p.BlockHash != e.BlockHash || p.TransferIdentity != e.Identity() || p.Recipient != e.To || p.RecipientSource != RecipientTransfer {
		return ErrIntegrity
	}
	if p.Actor == nil {
		if p.ActorSource != ActorUnknown {
			return ErrIntegrity
		}
		return nil
	}
	actor, err := Address(*p.Actor)
	if err != nil || actor == Zero || actor != *p.Actor || p.ActorSource != ActorChainEvent {
		return ErrIntegrity
	}
	return nil
}
