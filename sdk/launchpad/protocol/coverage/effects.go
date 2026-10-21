package coverage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type Projection struct {
	Balances                           map[string]*big.Int
	Supply                             map[common.Address]*big.Int
	NFTOwners                          map[string]common.Address
	CreatorLiability, OpticalLiability map[common.Address]*big.Int
	AirdropLiability                   map[string]*big.Int
	Claims                             []Effect
	Funding                            map[string]*big.Int
	Reserves                           map[string]*big.Int
	Lifecycle                          *Lifecycle
	seen                               map[string][32]byte
}

func NewProjection(lifecycle *Lifecycle) (*Projection, error) {
	if lifecycle == nil {
		return nil, fmt.Errorf("lifecycle required")
	}
	return &Projection{Balances: map[string]*big.Int{}, Supply: map[common.Address]*big.Int{}, NFTOwners: map[string]common.Address{}, CreatorLiability: map[common.Address]*big.Int{}, OpticalLiability: map[common.Address]*big.Int{}, AirdropLiability: map[string]*big.Int{}, Funding: map[string]*big.Int{}, Reserves: map[string]*big.Int{}, Lifecycle: lifecycle, seen: map[string][32]byte{}}, nil
}
func unsigned(value any) (*big.Int, error) {
	amount, ok := value.(*big.Int)
	if !ok || amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 {
		return nil, fmt.Errorf("native uint256 required")
	}
	return new(big.Int).Set(amount), nil
}
func change[K comparable](m map[K]*big.Int, key K, delta *big.Int) error {
	amount := new(big.Int)
	if old := m[key]; old != nil {
		amount.Set(old)
	}
	amount.Add(amount, delta)
	if amount.Sign() < 0 {
		return fmt.Errorf("projection baseline or liability missing")
	}
	m[key] = amount
	return nil
}
func cloneNumbers[K comparable](source map[K]*big.Int) map[K]*big.Int {
	out := make(map[K]*big.Int, len(source))
	for k, v := range source {
		out[k] = new(big.Int).Set(v)
	}
	return out
}
func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	out := make(map[K]V, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}
func (p *Projection) ApplyReceipt(result *ReceiptResult) error {
	if p == nil || p.Lifecycle == nil || result == nil || !result.Successful || result.seal == ([32]byte{}) || result.seal != receiptSeal(result) {
		return fmt.Errorf("unaltered correlated successful receipt required")
	}
	candidate := *p
	candidate.Balances = cloneNumbers(p.Balances)
	candidate.Supply = cloneNumbers(p.Supply)
	candidate.NFTOwners = cloneMap(p.NFTOwners)
	candidate.CreatorLiability = cloneNumbers(p.CreatorLiability)
	candidate.OpticalLiability = cloneNumbers(p.OpticalLiability)
	candidate.AirdropLiability = cloneNumbers(p.AirdropLiability)
	candidate.Funding = cloneNumbers(p.Funding)
	candidate.Reserves = cloneNumbers(p.Reserves)
	candidate.Claims = append([]Effect(nil), p.Claims...)
	candidate.seen = cloneMap(p.seen)
	lifecycle := *p.Lifecycle
	lifecycle.Orders = cloneMap(p.Lifecycle.Orders)
	lifecycle.Controls = cloneMap(p.Lifecycle.Controls)
	lifecycle.InvalidatedOpticals = cloneMap(p.Lifecycle.InvalidatedOpticals)
	candidate.Lifecycle = &lifecycle
	for _, e := range result.Effects {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if previous, exists := candidate.seen[e.ID]; exists {
			if previous != hash {
				return fmt.Errorf("effect identity conflict")
			}
			continue
		}
		if e.ID != effectID(e.Identity, e.Kind) || e.Identity.TransactionHash != result.TransactionHash || e.Identity.BlockHash != result.BlockHash || e.Identity.ChainID != 125 {
			return fmt.Errorf("effect provenance differs")
		}
		if err = candidate.apply(e); err != nil {
			return err
		}
		candidate.seen[e.ID] = hash
	}
	*p = candidate
	return nil
}
func (p *Projection) apply(e Effect) error {
	zero := common.Address{}
	if e.Kind == "asset_movement" {
		from, fok := e.Fields["from"].(common.Address)
		to, tok := e.Fields["to"].(common.Address)
		if !fok || !tok || (from == zero && to == zero) {
			return fmt.Errorf("native movement parties required")
		}
		if e.Role == "SidioraERC20" {
			amount, err := unsigned(e.Fields["value"])
			if err != nil {
				return err
			}
			if from != zero {
				if err = change(p.Balances, e.Identity.Emitter.Hex()+from.Hex(), new(big.Int).Neg(amount)); err != nil {
					return err
				}
			} else {
				if err = change(p.Supply, e.Identity.Emitter, amount); err != nil {
					return err
				}
			}
			if to != zero {
				if err = change(p.Balances, e.Identity.Emitter.Hex()+to.Hex(), amount); err != nil {
					return err
				}
			} else {
				if err = change(p.Supply, e.Identity.Emitter, new(big.Int).Neg(amount)); err != nil {
					return err
				}
			}
		} else if e.Role == "SidioraNFT" {
			token, err := unsigned(e.Fields["tokenId"])
			if err != nil {
				return err
			}
			key := e.Identity.Emitter.Hex() + "/" + token.String()
			if from != zero && p.NFTOwners[key] != from {
				return fmt.Errorf("NFT ownership baseline differs")
			}
			if from == zero && p.NFTOwners[key] != zero {
				return fmt.Errorf("NFT already minted")
			}
			if to == zero {
				delete(p.NFTOwners, key)
			} else {
				p.NFTOwners[key] = to
			}
		}
	}
	if e.Role == "FeeAccumulator" {
		pool, ok := e.Fields["pool"].(common.Address)
		if ok && pool != zero {
			switch e.Event {
			case "FeeRecorded":
				amount, err := unsigned(e.Fields["poolCut"])
				if err != nil {
					return err
				}
				if err = change(p.CreatorLiability, pool, amount); err != nil {
					return err
				}
			case "FeesClaimed", "FeesBurned", "LpRewardsSent":
				amount, err := unsigned(e.Fields["amount"])
				if err != nil {
					return err
				}
				if err = change(p.CreatorLiability, pool, new(big.Int).Neg(amount)); err != nil {
					return err
				}
				if e.Event == "FeesClaimed" {
					p.Claims = append(p.Claims, e)
				}
			case "OpticalSurplusRecorded":
				amount, err := unsigned(e.Fields["amount"])
				if err != nil {
					return err
				}
				if err = change(p.OpticalLiability, pool, amount); err != nil {
					return err
				}
			case "OpticalSurplusClaimed":
				amount, err := unsigned(e.Fields["amount"])
				if err != nil {
					return err
				}
				if err = change(p.OpticalLiability, pool, new(big.Int).Neg(amount)); err != nil {
					return err
				}
				p.Claims = append(p.Claims, e)
			case "AirdropTriggered":
				amount, err := unsigned(e.Fields["totalAmount"])
				if err != nil {
					return err
				}
				epoch, err := unsigned(e.Fields["epoch"])
				if err != nil {
					return err
				}
				if err = change(p.CreatorLiability, pool, new(big.Int).Neg(amount)); err != nil {
					return err
				}
				if err = change(p.AirdropLiability, pool.Hex()+"/"+epoch.String(), amount); err != nil {
					return err
				}
			case "AirdropClaimed":
				amount, err := unsigned(e.Fields["amount"])
				if err != nil {
					return err
				}
				epoch, err := unsigned(e.Fields["epoch"])
				if err != nil {
					return err
				}
				if err = change(p.AirdropLiability, pool.Hex()+"/"+epoch.String(), new(big.Int).Neg(amount)); err != nil {
					return err
				}
				p.Claims = append(p.Claims, e)
			}
		}
	}
	if e.Role == "PECORVault" && (e.Kind == "funding_movement" || e.Kind == "reserve_state") {
		token, _ := e.Fields["token"].(common.Address)
		native := e.Event == "NativeDeposit" || e.Event == "NativeWithdrawal"
		if !native && token == zero {
			return fmt.Errorf("registered vault token required")
		}
		reserve := e.Fields["newReserve"]
		if reserve == nil {
			reserve = e.Fields["newReserves"]
		}
		if reserve != nil {
			amount, err := unsigned(reserve)
			if err != nil {
				return err
			}
			p.Reserves[e.Identity.Emitter.Hex()+token.Hex()] = amount
		}
		if e.Kind == "funding_movement" {
			party, ok := e.Fields["depositor"].(common.Address)
			if !ok {
				party, ok = e.Fields["recipient"].(common.Address)
			}
			if !ok || party == zero {
				return fmt.Errorf("vault party required")
			}
			amount, err := unsigned(e.Fields["amount"])
			if err != nil {
				return err
			}
			if e.Event != "Deposit" && e.Event != "NativeDeposit" {
				amount.Neg(amount)
			}
			if err = change(p.Funding, e.Identity.Emitter.Hex()+token.Hex()+party.Hex(), amount); err != nil {
				return err
			}
		}
	}
	return p.Lifecycle.apply(e)
}
