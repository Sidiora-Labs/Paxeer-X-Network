package policy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
)

const (
	KindLXActivity = "lx_activity"
	KindLXBind     = "lx_bind"
	KindLXGrant    = "lx_grant"

	KindOperatorVerification = "operator_verification"
	VerificationDomain       = "LX:PAXEER-CEREMONY-VERIFY:v1"

	CodeVerificationView     = "verification_view"
	CodeVerificationIsolated = "verification_isolated"
)

type Verification struct {
	KeyID         string
	PublicKey     []byte
	ImportSession string
	Message       []byte
}

func VerificationMessage(keyID string, publicKey []byte, importSession string) []byte {
	out := []byte(VerificationDomain)
	for _, field := range [][]byte{[]byte(keyID), publicKey, []byte(importSession)} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(field)))
		out = append(out, field...)
	}
	return out
}

func inspectVerification(_ Context, view any) (Inspection, error) {
	v, ok := view.(*Verification)
	if !ok || v == nil {
		return Inspection{}, refuse(CodeVerificationView, "operator verification accepts only the verification message")
	}
	if v.KeyID == "" || len(v.PublicKey) == 0 || v.ImportSession == "" || len(v.KeyID) > 0xffff || len(v.PublicKey) > 0xffff || len(v.ImportSession) > 0xffff {
		return Inspection{}, refuse(CodeVerificationView, "verification message fields are missing or oversized")
	}
	if !bytes.Equal(v.Message, VerificationMessage(v.KeyID, v.PublicKey, v.ImportSession)) {
		return Inspection{}, refuse(CodeVerificationView, "message is not the verification message of the imported key")
	}
	return Inspection{}, nil
}

func CarriesVerification(view any) bool {
	domain := []byte(VerificationDomain)
	switch v := view.(type) {
	case *Verification:
		return true
	case []byte:
		return bytes.Contains(v, domain)
	case *evm.PersonalMessage:
		return v != nil && bytes.Contains(v.Message, domain)
	case *evm.Transaction:
		return v != nil && bytes.Contains(v.Data, domain)
	case *evm.TypedData:
		if v == nil {
			return false
		}
		raw, err := json.Marshal(v.Data)
		return err != nil || bytes.Contains(raw, domain)
	case *evm.SponsoredBatchClaim:
		if v == nil {
			return false
		}
		for _, call := range v.Batch.Calls {
			if bytes.Contains(call.Data, domain) {
				return true
			}
		}
	}
	return false
}

func (p *Policy) evaluateVerification(req Request) (Decision, bool) {
	if req.Kind != KindOperatorVerification {
		if CarriesVerification(req.View) {
			return denied(CodeVerificationIsolated, "the verification message is signed only under %s", KindOperatorVerification), true
		}
		return Decision{}, false
	}
	inspect, ok := p.inspector(KindOperatorVerification)
	if !ok {
		return denied(CodeUnknownKind, "request kind %q is not known", req.Kind), true
	}
	if _, err := inspect(Context{}, req.View); err != nil {
		var refusal *Refusal
		if errors.As(err, &refusal) {
			return denied(refusal.Code, "%s", refusal.Reason), true
		}
		return denied(CodeDecodeError, "%v", err), true
	}
	return Decision{Allowed: true, Code: CodeAllowed, Reason: "request is the verification message of an imported key"}, true
}

func KernelKinds() []string {
	return []string{KindLXActivity, KindLXBind, KindLXGrant}
}

func (p *Policy) RegisterKernel(activity, bind, grant Inspector) error {
	if activity == nil || bind == nil || grant == nil {
		return errors.New("register kernel kinds: every inspector is required")
	}
	inspectors := map[string]Inspector{KindLXActivity: activity, KindLXBind: bind, KindLXGrant: grant}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, kind := range KernelKinds() {
		if _, exists := p.kinds[kind]; exists {
			return fmt.Errorf("register kernel kinds: kind %q is already registered", kind)
		}
	}
	for _, kind := range KernelKinds() {
		p.kinds[kind] = inspectors[kind]
	}
	return nil
}
