// Package types holds the wire shapes for media/metadata: the public metadata
// response (token/pool DNA) and the multipart upload request/response. Field
// names mirror the TS public contract (snake_case) so the frontend SDK keeps
// working across the strangler cutover.
package types

// Socials is the structured social-links block.
type Socials struct {
	Website  *string `json:"website"`
	Twitter  *string `json:"twitter"`
	Telegram *string `json:"telegram"`
	Discord  *string `json:"discord"`
}

// Images holds absolute URLs to the token logo + banner (nil when absent).
type Images struct {
	Logo   *string `json:"logo"`
	Banner *string `json:"banner"`
}

// PublicMetadata is the canonical public token/pool DNA response.
type PublicMetadata struct {
	TokenAddress string   `json:"token_address"`
	PoolAddress  *string  `json:"pool_address"`
	Name         *string  `json:"name"`
	Symbol       *string  `json:"symbol"`
	Decimals     int32    `json:"decimals"`
	TotalSupply  string   `json:"total_supply"`
	Creator      *string  `json:"creator"`
	Description  *string  `json:"description"`
	Socials      Socials  `json:"socials"`
	Tags         []string `json:"tags"`
	Images       Images   `json:"images"`
	CreatedAt    *int64   `json:"created_at"`
	UpdatedAt    *int64   `json:"updated_at"`
}

// UploadMetadata is the JSON payload sent in the multipart "metadata" field.
type UploadMetadata struct {
	Name        string   `json:"name"`
	Symbol      string   `json:"symbol"`
	Description string   `json:"description"`
	Website     string   `json:"website"`
	Twitter     string   `json:"twitter"`
	Telegram    string   `json:"telegram"`
	Discord     string   `json:"discord"`
	Tags        []string `json:"tags"`
	Decimals    *int32   `json:"decimals"`
}

// UploadResponse is returned by POST /metadata/{tokenAddress}.
type UploadResponse struct {
	Success         bool     `json:"success"`
	MetadataUpdated bool     `json:"metadata_updated"`
	LogoURL         *string  `json:"logo_url"`
	BannerURL       *string  `json:"banner_url"`
	Errors          []string `json:"errors,omitempty"`
}
