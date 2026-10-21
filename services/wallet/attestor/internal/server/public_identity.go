package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
	"github.com/ethereum/go-ethereum/common"
)

const PathPublicWallet = "/v1/keys/public-wallet"

type publicWalletRequest struct {
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Owner     string `json:"owner"`
}

type publicWalletResponse struct {
	NodeID          string `json:"node_id"`
	KeyID           string `json:"key_id"`
	PublicKey       string `json:"public_key"`
	Owner           string `json:"owner"`
	WalletKeyID     string `json:"wallet_key_id"`
	WalletPublicKey string `json:"wallet_public_key"`
	WalletEpoch     uint64 `json:"wallet_epoch"`
	Address         string `json:"address"`
	AuditSequence   uint64 `json:"audit_sequence"`
}

func (s *Server) publicIdentityOwner(keyID string) (string, string, *Error) {
	var holder struct {
		Owner   string `json:"owner"`
		Account string `json:"account"`
	}
	if err := s.opts.Store.WithShare(keyID, func(plain []byte) error { return json.Unmarshal(plain, &holder) }); err != nil {
		return "", "", newError(CodeStoreFailed, "held identity metadata unavailable")
	}
	if holder.Owner == "" || !common.IsHexAddress(holder.Account) {
		return "", "", newError(CodeKeyInvalidShare, "held identity metadata is incomplete")
	}
	return holder.Owner, holder.Account, nil
}

func (s *Server) HandlePublicWallet(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var response publicWalletResponse
	if e == nil {
		response, e = s.doPublicWallet(r, body)
	}
	s.finish(w, "keys.public-wallet", body, response, e)
}

func (s *Server) doPublicWallet(r *http.Request, body []byte) (publicWalletResponse, *Error) {
	var req publicWalletRequest
	if e := decodeRequest(body, &req); e != nil {
		return publicWalletResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return publicWalletResponse{}, e
	}
	refuse := func(e *Error) (publicWalletResponse, *Error) {
		return publicWalletResponse{}, s.deny("keys.public-wallet", req.KeyID, req.Owner, "denied", req.SessionID, e)
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get(HeaderAgentKey) != "" {
		return refuse(newError(CodeTokenMissing, "the human bearer assertion is required"))
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	primary, err := s.opts.Store.Get(req.KeyID)
	if err != nil {
		return refuse(newError(CodeKeyNotFound, "primary identity is not held"))
	}
	public, err := hex.DecodeString(req.PublicKey)
	if err != nil || len(public) != 32 || primary.Curve != store.CurveEd25519 || !bytes.Equal(public, primary.PublicKey) {
		return refuse(newError(CodeKeyInvalidShare, "primary identity public key differs"))
	}
	owner, account, e := s.publicIdentityOwner(req.KeyID)
	if e != nil {
		return refuse(e)
	}
	if req.Owner != owner {
		return refuse(newError(CodeTokenNotOwner, "requested owner differs from held identity"))
	}
	subject, e := s.authenticateAt(r, PathPublicWallet, req.KeyID, body, owner)
	if e != nil {
		return refuse(e)
	}
	if subject != owner {
		return refuse(newError(CodeTokenNotOwner, "assertion must name the primary human owner"))
	}
	if err := s.opts.Inventory.Check(req.KeyID, "sign", owner, primary.Curve, &primary); err != nil {
		return refuse(newError(CodeKeyInvalidShare, "approved inventory does not admit primary identity"))
	}
	records, err := s.opts.Store.List()
	if err != nil {
		return refuse(newError(CodeStoreFailed, "wallet identity inventory unavailable"))
	}
	var wallet *store.ShareRecord
	for _, record := range records {
		if record.Curve != store.CurveSecp256k1 {
			continue
		}
		heldOwner, _, e := s.publicIdentityOwner(record.KeyID)
		if e != nil {
			return refuse(e)
		}
		if heldOwner != owner {
			continue
		}
		if wallet != nil {
			return refuse(newError(CodeKeyInvalidShare, "owner has ambiguous wallet identity"))
		}
		copy := record
		wallet = &copy
	}
	if wallet == nil {
		return refuse(newError(CodeKeyNotFound, "owner has no held wallet identity"))
	}
	unlockWallet := s.lockKey(wallet.KeyID)
	defer unlockWallet()
	held, err := s.opts.Store.Get(wallet.KeyID)
	if err != nil {
		return refuse(newError(CodeKeyNotFound, "wallet identity is no longer held"))
	}
	wallet = &held
	walletOwner, walletAccount, e := s.publicIdentityOwner(wallet.KeyID)
	if e != nil {
		return refuse(e)
	}
	if walletOwner != owner || wallet.Curve != store.CurveSecp256k1 {
		return refuse(newError(CodeKeyInvalidShare, "wallet owner binding changed"))
	}
	if err := s.opts.Inventory.Check(wallet.KeyID, "sign", owner, wallet.Curve, wallet); err != nil {
		return refuse(newError(CodeKeyInvalidShare, "approved inventory does not admit wallet identity"))
	}
	point, err := parsePublicKey(dealer.Secp256k1, hex.EncodeToString(wallet.PublicKey))
	if err != nil {
		return refuse(newError(CodeKeyInvalidShare, "held wallet public key is invalid"))
	}
	address := secpAddress(point)
	if !strings.EqualFold(address, walletAccount) || !strings.EqualFold(address, account) || common.HexToAddress(address) == (common.Address{}) {
		return refuse(newError(CodeKeyInvalidShare, "primary policy account and held wallet identity differ"))
	}
	sequence, e := s.audit("keys.public-wallet", req.KeyID, owner, "allowed", "resolved held owner wallet identity", req.SessionID)
	if e != nil {
		return publicWalletResponse{}, e
	}
	return publicWalletResponse{NodeID: s.opts.NodeID, KeyID: req.KeyID, PublicKey: hex.EncodeToString(primary.PublicKey), Owner: owner, WalletKeyID: wallet.KeyID, WalletPublicKey: hex.EncodeToString(wallet.PublicKey), WalletEpoch: wallet.Epoch, Address: strings.ToLower(address), AuditSequence: sequence}, nil
}
