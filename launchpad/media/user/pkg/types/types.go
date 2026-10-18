// Package types holds the wire shapes for media/user: the public profile
// response (profile + socials + images + created-pools), the watchlist response,
// and the profile-update / image-upload request+response shapes.
//
// The contract is clean, consistent snake_case (the frontend is being rebuilt
// against this Go service), mirroring the media/metadata PublicMetadata style.
package types

// Socials is the structured social-links block (nil field = unset).
type Socials struct {
	Website  *string `json:"website"`
	Twitter  *string `json:"twitter"`
	Telegram *string `json:"telegram"`
	Discord  *string `json:"discord"`
}

// Images holds absolute URLs to the user avatar + banner (nil when absent).
type Images struct {
	Avatar *string `json:"avatar"`
	Banner *string `json:"banner"`
}

// CreatedPool is one pool created by the wallet (from the indexer cross-read).
type CreatedPool struct {
	PoolAddress  string `json:"pool_address"`
	TokenAddress string `json:"token_address"`
	CreatedAt    int64  `json:"created_at"`
}

// PublicProfile is the canonical public user profile response.
type PublicProfile struct {
	WalletAddress string        `json:"wallet_address"`
	DisplayName   *string       `json:"display_name"`
	Bio           *string       `json:"bio"`
	Socials       Socials       `json:"socials"`
	Images        Images        `json:"images"`
	CreatedPools  []CreatedPool `json:"created_pools"`
	CreatedAt     *int64        `json:"created_at"`
	UpdatedAt     *int64        `json:"updated_at"`
}

// ProfileData is the editable profile payload (POST /users/{wallet}).
type ProfileData struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	Website     string `json:"website"`
	Twitter     string `json:"twitter"`
	Telegram    string `json:"telegram"`
	Discord     string `json:"discord"`
}

// UpdateProfileRequest is the EIP-191-signed profile-update body.
type UpdateProfileRequest struct {
	Data      ProfileData `json:"data"`
	Signature string      `json:"signature"`
	Message   string      `json:"message"`
}

// WatchlistEntry is one watched pool.
type WatchlistEntry struct {
	PoolAddress string `json:"pool_address"`
	AddedAt     int64  `json:"added_at"`
}

// WatchlistResponse is returned by GET /users/{wallet}/watchlist.
type WatchlistResponse struct {
	WalletAddress string           `json:"wallet_address"`
	Pools         []WatchlistEntry `json:"pools"`
}

// WatchlistMutateRequest is the EIP-191-signed body for PUT/DELETE watchlist.
type WatchlistMutateRequest struct {
	Signature string `json:"signature"`
	Message   string `json:"message"`
}

// SuccessResponse is the minimal success acknowledgement.
type SuccessResponse struct {
	Success bool `json:"success"`
}

// UploadResponse is returned by an image upload (avatar/banner).
type UploadResponse struct {
	Success bool   `json:"success"`
	URL     string `json:"url"`
}
