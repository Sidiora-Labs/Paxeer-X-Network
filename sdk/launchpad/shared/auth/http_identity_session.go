package auth

import "context"

// CheckStreamSession confirms through the identity service's
// /internal/v2/identity/session:check that the session behind p is still live,
// with the same principal validation as MTLSIdentityAuthority. It makes
// HTTPIdentityAuthority satisfy the request-plus-session authority interfaces
// (for example core/pnl-tracker's l16.LiveIdentityAuthority) that the bearer
// service-token transport already serves through HTTPStreamAuthority.
func (a HTTPIdentityAuthority) CheckStreamSession(ctx context.Context, p StreamPrincipal) error {
	if !validIdentityID(p.AccountID) || !validIdentityID(p.SessionID) || p.AuthEpoch < 0 {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet {
		return ErrScopeDenied
	}
	return HTTPStreamAuthority(a).CheckStreamSession(ctx, p)
}
