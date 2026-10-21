package auth

import (
	"context"
	"encoding/hex"
	"strings"
)

// ConsumeChatStreamTicket redeems a single-use chat stream ticket at the
// identity service's /internal/v2/identity/chat-ticket:consume over the bearer
// service-token transport, with the same ticket shape and binding validation
// as MTLSIdentityAuthority.ConsumeChatStreamTicket. Together with
// CheckStreamSession it makes HTTPStreamAuthority a ChatStreamAuthority for
// services that reach the identity authority over a private network instead
// of mTLS (media/social's platform runtime).
func (a HTTPStreamAuthority) ConsumeChatStreamTicket(ctx context.Context, ticket string, binding StreamTicketBinding) (StreamPrincipal, error) {
	if len(ticket) != 64 || strings.ToLower(ticket) != ticket {
		return StreamPrincipal{}, ErrScopeDenied
	}
	if decoded, err := hex.DecodeString(ticket); err != nil || len(decoded) != 32 {
		return StreamPrincipal{}, ErrScopeDenied
	}
	var p StreamPrincipal
	if err := a.call(ctx, "/internal/v2/identity/chat-ticket:consume", struct {
		Ticket  string              `json:"ticket"`
		Binding StreamTicketBinding `json:"binding"`
	}{ticket, binding}, &p); err != nil {
		return StreamPrincipal{}, err
	}
	if ValidateChatStreamBinding(p, binding) != nil {
		return StreamPrincipal{}, ErrScopeDenied
	}
	return p, nil
}
