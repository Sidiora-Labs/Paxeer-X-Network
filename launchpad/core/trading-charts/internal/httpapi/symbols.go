package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

func parseSymbolQuery(raw string) (string, error) {
	if len(raw) > 16384 {
		return "", errors.New("symbol query too large")
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		return "", errors.New("malformed symbol query")
	}
	for key, values := range query {
		if len(values) != 1 {
			return "", errors.New("duplicate symbol query field")
		}
		if key != "symbol" && key != "chainId" {
			return "", errors.New("unknown symbol query field")
		}
	}
	if query.Has("chainId") && query.Get("chainId") != "125" {
		return "", errors.New("chainId must be 125")
	}
	symbol := strings.ToLower(query.Get("symbol"))
	if !poolAddressPattern.MatchString(symbol) {
		return "", errors.New("symbol must be a pool address")
	}
	return symbol, nil
}

func udfSymbols(w http.ResponseWriter, r *http.Request) {
	symbol, err := parseSymbolQuery(r.URL.RawQuery)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", err.Error(), false)
		return
	}
	sharedhttp.WriteJSON(w, 200, map[string]interface{}{
		"name":                   symbol,
		"full_name":              symbol + "/USDL",
		"description":            "Token " + symbol,
		"type":                   "token",
		"session":                "24x7",
		"exchange":               "sidiora",
		"listed_exchange":        "sidiora",
		"timezone":               "Etc/UTC",
		"has_intraday":           true,
		"has_weekly_and_monthly": true,
		"supported_resolutions":  supportedResolutions(),
		"pricescale":             100000000,
		"ticker":                 symbol,
		"currency_code":          "USDL",
		"has_empty_bars":         false,
	})
}

type PoolMetadataClient struct {
	Endpoint string
	Client   *http.Client
}

var ErrUnknownPool = errors.New("unknown verified pool")

func boundedChartJSON(ctx context.Context, client *http.Client, endpoint string, dst any, schema string) error {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 1500 * time.Millisecond}
	}
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("chart upstream redirect refused") }
	response, err := boundedClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if response.StatusCode == 404 {
		return ErrUnknownPool
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("chart upstream unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	if err != nil || len(raw) > 16*1024*1024 {
		return errors.New("chart upstream response too large")
	}
	if interfaces.ValidateJSON(schema, raw) != nil {
		return errors.New("invalid frozen chart upstream response")
	}
	return json.Unmarshal(raw, dst)
}
func (c PoolMetadataClient) Resolve(ctx context.Context, pool string) (interfaces.PoolDetailResponse, error) {
	var out interfaces.PoolDetailResponse
	base, err := url.Parse(c.Endpoint)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") || !poolAddressPattern.MatchString(pool) {
		return out, errors.New("metadata authority unavailable")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v2/pools/" + pool
	if err = boundedChartJSON(ctx, c.Client, base.String(), &out, "PoolDetailResponse"); err != nil {
		return out, err
	}
	p, t := out.Data.Pool, out.Data.Token
	if p.ChainId != "125" || strings.ToLower(p.PoolAddress) != pool || !strings.EqualFold(p.TokenAddress, t.Address) || t.Decimals == nil || t.DecimalsQuality != "verified" || t.SourcePosition == nil || t.SourcePosition.ChainId != "125" || p.QuoteAssetDecimals != 6 || *t.Decimals < 0 || *t.Decimals > 255 {
		return out, errors.New("unverified pool metadata")
	}
	return out, nil
}

type SymbolsHandler struct {
	Metadata  PoolMetadataClient
	Canonical *CanonicalPoolMetadataSource
	// Development is the non-production resolver (development indexer + RPC).
	// Production never sets it.
	Development      TokenMetadataResolver
	DisplayPrecision int
}

func (h SymbolsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pool, err := parseSymbolQuery(r.URL.RawQuery)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", err.Error(), false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var token interfaces.Token
	quoteDecimals := interfaces.SafeInteger(6)
	if h.Development != nil {
		token, err = h.Development.Resolve(ctx, pool)
	} else if h.Canonical != nil {
		token, err = h.Canonical.Resolve(ctx, pool)
	} else {
		var metadata interfaces.PoolDetailResponse
		metadata, err = h.Metadata.Resolve(ctx, pool)
		token = metadata.Data.Token
		quoteDecimals = metadata.Data.Pool.QuoteAssetDecimals
	}
	if errors.Is(err, ErrUnknownPool) {
		historyError(w, 404, "NOT_FOUND", "unknown pool", false)
		return
	}
	if err != nil || ctx.Err() != nil {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "verified metadata unavailable", true)
		return
	}
	precision := h.DisplayPrecision
	if precision == 0 {
		precision = 8
	}
	if precision < 1 || precision > 12 {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "invalid vendor precision", false)
		return
	}
	if !boundedMetadataText(token.Name, 160) || !boundedMetadataText(token.Symbol, 32) {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "invalid token metadata text", false)
		return
	}
	scope, err := json.Marshal([]any{"symbols-v2", "125", pool, token, precision, quoteDecimals})
	if err != nil || token.Decimals == nil || token.SourcePosition == nil {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "invalid verified token metadata", false)
		return
	}
	digest := sha256.Sum256(scope)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	scale := int64(1)
	for i := 0; i < precision; i++ {
		scale *= 10
	}
	sharedhttp.WriteJSON(w, 200, map[string]any{"name": pool, "ticker": pool, "full_name": token.Symbol + "/USDL", "description": token.Name, "type": "token", "session": "24x7", "exchange": "sidiora", "listed_exchange": "sidiora", "timezone": "Etc/UTC", "has_intraday": true, "has_weekly_and_monthly": true, "supported_resolutions": supportedResolutions(), "pricescale": scale, "priceScale": scale, "displayPrecision": precision, "minmov": 1, "currency_code": "USDL", "has_empty_bars": false, "chainId": "125", "poolAddress": pool, "tokenAddress": token.Address, "baseDecimals": *token.Decimals, "quoteDecimals": quoteDecimals, "metadataRevision": token.MetadataRevision})
}
func boundedMetadataText(value string, max int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '<' || r == '>' {
			return false
		}
	}
	return true
}

// CanonicalPoolMetadataSource resolves a pool's token metadata from the
// indexer's canonical creation fact, read from the chain at the creation
// block. Chain reads are bound to the node-reported block hash (chainread):
// on Paxeer the indexer records CometBFT block hashes.
type CanonicalPoolMetadataSource struct {
	Pool  *pgxpool.Pool
	Chain *chainread.Reader
}

func (s CanonicalPoolMetadataSource) Resolve(ctx context.Context, pool string) (interfaces.Token, error) {
	var token interfaces.Token
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.Pool == nil || s.Chain == nil || !poolAddressPattern.MatchString(pool) {
		return token, errors.New("canonical metadata authority unavailable")
	}
	var raw []byte
	err := s.Pool.QueryRow(ctx, "SELECT payload FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1", pool).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return token, ErrUnknownPool
	}
	if err != nil {
		return token, err
	}
	var fact struct {
		Token      string                   `json:"token"`
		Pool       string                   `json:"pool"`
		Position   interfaces.ChainPosition `json:"position"`
		Provenance struct {
			Receipt string `json:"receiptSHA256"`
			Header  string `json:"headerSHA256"`
		} `json:"provenance"`
	}
	if len(raw) > 1024*1024 || json.Unmarshal(raw, &fact) != nil || !strings.EqualFold(fact.Pool, pool) || !poolAddressPattern.MatchString(fact.Token) || fact.Position.ChainId != "125" || len(fact.Provenance.Receipt) != 64 || len(fact.Provenance.Header) != 64 {
		return token, errors.New("invalid canonical creation authority")
	}
	if _, err = hex.DecodeString(fact.Provenance.Receipt); err != nil {
		return token, err
	}
	if _, err = hex.DecodeString(fact.Provenance.Header); err != nil {
		return token, err
	}
	height, err := strconv.ParseUint(string(fact.Position.Height), 10, 63)
	if err != nil {
		return token, err
	}
	blockHash := strings.ToLower(fact.Position.BlockHash)
	address := common.HexToAddress(fact.Token)
	values, err := s.Chain.CallAt(ctx, int64(height), blockHash, []ethereum.CallMsg{{To: &address, Data: []byte{0x06, 0xfd, 0xde, 0x03}}, {To: &address, Data: []byte{0x95, 0xd8, 0x9b, 0x41}}, {To: &address, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}, {To: &address, Data: []byte{0x18, 0x16, 0x0d, 0xdd}}})
	if err != nil || len(values) != 4 || len(values[2]) != 32 || len(values[3]) != 32 {
		return token, errors.New("pinned token metadata unavailable")
	}
	name, err := decodeMetadataString(values[0], 160)
	if err != nil {
		return token, err
	}
	symbol, err := decodeMetadataString(values[1], 32)
	if err != nil {
		return token, err
	}
	decimalValue := new(big.Int).SetBytes(values[2])
	if !decimalValue.IsInt64() || decimalValue.Int64() > 255 {
		return token, errors.New("invalid canonical token decimals")
	}
	decimals := interfaces.SafeInteger(decimalValue.Int64())
	total := interfaces.UInt256(new(big.Int).SetBytes(values[3]).String())
	digest := sha256.Sum256(append(append(append([]byte(pool+":"+fact.Token+":"+blockHash), values[0]...), values[1]...), append(values[2], values[3]...)...))
	revision := interfaces.UInt256(new(big.Int).SetBytes(digest[:]).String())
	token = interfaces.Token{Address: strings.ToLower(fact.Token), Name: name, Symbol: symbol, Decimals: &decimals, DecimalsQuality: "verified", MetadataRevision: revision, SourcePosition: &fact.Position, SupplyQuality: "verified", TotalSupplyRaw: &total}
	return token, nil
}
func decodeMetadataString(raw []byte, max int) (string, error) {
	if len(raw) < 64 || len(raw) > 1024 {
		return "", errors.New("invalid token ABI text")
	}
	offset := new(big.Int).SetBytes(raw[:32])
	size := new(big.Int).SetBytes(raw[32:64])
	if !offset.IsInt64() || offset.Int64() != 32 || !size.IsInt64() || size.Int64() < 1 || size.Int64() > int64(len(raw)-64) {
		return "", errors.New("invalid token ABI text bounds")
	}
	value := string(raw[64 : 64+int(size.Int64())])
	if !boundedMetadataText(value, max) {
		return "", errors.New("invalid token metadata text")
	}
	return value, nil
}
