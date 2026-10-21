package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
)

// ErrChainUnavailable reports that the development chain RPC is not
// configured or could not answer; the v2 reads map it to a JSON 503.
var ErrChainUnavailable = errors.New("development chain reference unavailable")

// DevelopmentReceipts is the chain read the development v2 adapter needs:
// the receipt of the pool-creation transaction the development indexer
// stored. *ethclient.Client satisfies it.
type DevelopmentReceipts interface {
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
}

const developmentTokenCacheSize = 8192

// DevelopmentV2Reads serves the public v2 token reads outside production.
// Production wires V2ContractHandlers/ConnectedMedia (canonical stream +
// v2 documents); the development deployment only runs the legacy indexer and
// the legacy metadata tables, so this adapter resolves the same response
// contract from indexer.pools, metadata.token_metadata and the TokenDeployed
// log in the pool's creation receipt (name, symbol, decimals, supply and the
// source position all come from that chain log, never from defaults).
type DevelopmentV2Reads struct {
	Pool  *pgxpool.Pool
	Chain DevelopmentReceipts // nil when RPC_URL is unset: /metadata answers 503
	Clock func() time.Time
	// Connected, when set, is the development-wired production media store
	// (see DevelopmentConnectedSchema). Tokens with a published v2 document
	// are then served by the production ConnectedMedia read handlers, and
	// resolved chain identities persist in its development_canonical_tokens
	// table so reads survive the pruned RPC dropping old receipts.
	Connected *ConnectedMedia

	mu     *sync.Mutex
	tokens map[string]iface.Token
}

// NewDevelopmentV2Reads builds the development v2 read adapter.
func NewDevelopmentV2Reads(pool *pgxpool.Pool, chain DevelopmentReceipts, clock func() time.Time) *DevelopmentV2Reads {
	if clock == nil {
		clock = time.Now
	}
	return &DevelopmentV2Reads{Pool: pool, Chain: chain, Clock: clock, mu: &sync.Mutex{}, tokens: map[string]iface.Token{}}
}

// RegisterRoutes mounts the public v2 GET routes the web client reads.
func (d *DevelopmentV2Reads) RegisterRoutes(r chi.Router) {
	r.Get("/v2/tokens/{tokenAddress}/metadata", d.metadata)
	r.Get("/v2/tokens/{tokenAddress}/media", d.media)
}

type developmentPoolRow struct {
	Pool, Token, Creator, TxHash string
	CreatedBlock                 int64
}

type developmentQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (d *DevelopmentV2Reads) poolRow(ctx context.Context, token string) (developmentPoolRow, error) {
	return developmentPoolRowFrom(ctx, d.Pool, token)
}

func developmentPoolRowFrom(ctx context.Context, q developmentQuerier, token string) (developmentPoolRow, error) {
	var out developmentPoolRow
	rows, err := q.Query(ctx, `SELECT lower(pool_address),lower(token_address),lower(creator),lower(tx_hash),created_block FROM indexer.pools WHERE lower(token_address)=$1 LIMIT 2`, token)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		if err = rows.Scan(&out.Pool, &out.Token, &out.Creator, &out.TxHash, &out.CreatedBlock); err != nil {
			return out, err
		}
		found++
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if found == 0 {
		return out, ErrMissing
	}
	if found > 1 || !addressRE.MatchString(out.Pool) || !addressRE.MatchString(out.Creator) || out.CreatedBlock < 0 {
		return out, ErrUnrepresentable
	}
	return out, nil
}

// legacyData maps the legacy metadata row onto the v2 domain record. A
// token with no legacy row reads as revision 0 with an empty display, the
// same shape production serves before the creator's first edit.
func (d *DevelopmentV2Reads) legacyData(ctx context.Context, row developmentPoolRow) (V2Data, bool, error) {
	data := V2Data{ChainID: 125, PoolAddress: row.Pool, TokenAddress: row.Token, Revision: "0", Display: V2Display{Tags: []string{}, Socials: []V2Social{}}, Source: "creator", ModerationStatus: "allowed"}
	meta, err := sqlcdb.New(d.Pool).GetTokenMetadata(ctx, row.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return data, false, nil
	}
	if err != nil {
		return data, false, err
	}
	data.Display.Name = boundedText(meta.Name, 80)
	data.Display.Symbol = boundedText(meta.Symbol, 16)
	if meta.Description.Valid && utf8.RuneCountInString(meta.Description.String) <= 4096 {
		data.Display.Description = meta.Description.String
	}
	seen := map[string]bool{}
	for _, tag := range parseTags(meta.CustomTags) {
		if tag == "" || utf8.RuneCountInString(tag) > 256 || seen[tag] || len(data.Display.Tags) == 20 {
			continue
		}
		seen[tag] = true
		data.Display.Tags = append(data.Display.Tags, tag)
	}
	for _, link := range []struct {
		label string
		value pgtype.Text
	}{{"Website", meta.Website}, {"X", meta.Twitter}, {"Telegram", meta.Telegram}, {"Discord", meta.Discord}} {
		if link.value.Valid && validHTTPS(link.value.String) {
			data.Display.Socials = append(data.Display.Socials, V2Social{Label: link.label, URL: link.value.String})
		}
	}
	if meta.UpdatedAt > 0 {
		data.Revision = strconv.FormatInt(meta.UpdatedAt, 10)
		updated := time.UnixMilli(meta.UpdatedAt).UTC()
		data.UpdatedAt = &updated
	}
	return data, true, nil
}

func boundedText(t pgtype.Text, max int) *string {
	if !t.Valid {
		return nil
	}
	n := utf8.RuneCountInString(t.String)
	if n == 0 || n > max {
		return nil
	}
	v := t.String
	return &v
}

// chainToken resolves the contract Token from the TokenDeployed log the
// factory emits in the pool-creation transaction. Resolved tokens are
// immutable and cached.
func (d *DevelopmentV2Reads) chainToken(ctx context.Context, row developmentPoolRow) (iface.Token, error) {
	d.mu.Lock()
	cached, ok := d.tokens[row.Token]
	d.mu.Unlock()
	if ok {
		return cached, nil
	}
	if token, found, err := d.persistedToken(ctx, row); err != nil {
		return iface.Token{}, err
	} else if found {
		d.remember(row.Token, token)
		return token, nil
	}
	if d.Chain == nil {
		return iface.Token{}, ErrChainUnavailable
	}
	if len(row.TxHash) != 66 || !strings.HasPrefix(row.TxHash, "0x") {
		return iface.Token{}, ErrUnrepresentable
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	receipt, err := d.Chain.TransactionReceipt(rctx, common.HexToHash(row.TxHash))
	if err != nil || receipt == nil {
		return iface.Token{}, ErrChainUnavailable
	}
	if receipt.Status != types.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.Cmp(big.NewInt(row.CreatedBlock)) != 0 {
		return iface.Token{}, ErrUnrepresentable
	}
	parsed, err := abi.JSON(strings.NewReader(canonicalEventsABI))
	if err != nil {
		return iface.Token{}, err
	}
	event := parsed.Events["TokenDeployed"]
	want := []common.Hash{event.ID, common.BytesToHash(common.HexToAddress(row.Token).Bytes()), common.BytesToHash(common.HexToAddress(row.Pool).Bytes()), common.BytesToHash(common.HexToAddress(row.Creator).Bytes())}
	var match *types.Log
	for _, l := range receipt.Logs {
		if l == nil || l.Removed || len(l.Topics) != 4 {
			continue
		}
		equal := true
		for i := range want {
			equal = equal && l.Topics[i] == want[i]
		}
		if !equal {
			continue
		}
		if match != nil {
			return iface.Token{}, ErrUnrepresentable
		}
		match = l
	}
	if match == nil || match.BlockNumber != uint64(row.CreatedBlock) || !strings.EqualFold(match.TxHash.Hex(), row.TxHash) {
		return iface.Token{}, ErrUnrepresentable
	}
	values := map[string]any{}
	if err = event.Inputs.NonIndexed().UnpackIntoMap(values, match.Data); err != nil {
		return iface.Token{}, ErrUnrepresentable
	}
	name, okName := values["name"].(string)
	symbol, okSymbol := values["symbol"].(string)
	decimals, okDecimals := values["decimals"].(uint8)
	supply, okSupply := values["totalSupply"].(*big.Int)
	if !okName || !okSymbol || !okDecimals || !okSupply || supply.Sign() < 0 || !canonicalUint(values["blockNumber"], strconv.FormatInt(row.CreatedBlock, 10)) {
		return iface.Token{}, ErrUnrepresentable
	}
	position := map[string]string{"chainId": "125", "height": strconv.FormatUint(match.BlockNumber, 10), "transactionIndex": strconv.FormatUint(uint64(match.TxIndex), 10), "logIndex": strconv.FormatUint(uint64(match.Index), 10), "transactionHash": strings.ToLower(match.TxHash.Hex()), "blockHash": strings.ToLower(match.BlockHash.Hex())}
	raw, err := json.Marshal(map[string]any{"address": row.Token, "name": name, "symbol": symbol, "decimals": decimals, "decimalsQuality": "verified", "totalSupplyRaw": supply.String(), "supplyQuality": "verified", "metadataRevision": "0", "logoAssetId": nil, "sourcePosition": position})
	if err != nil {
		return iface.Token{}, err
	}
	token, err := iface.DecodeToken(raw)
	if err != nil {
		return iface.Token{}, ErrUnrepresentable
	}
	d.persistToken(ctx, row, token)
	d.remember(row.Token, token)
	return token, nil
}

func (d *DevelopmentV2Reads) remember(address string, token iface.Token) {
	d.mu.Lock()
	if len(d.tokens) >= developmentTokenCacheSize {
		d.tokens = map[string]iface.Token{}
	}
	d.tokens[address] = token
	d.mu.Unlock()
}

func developmentToken(r *http.Request) (string, bool) {
	token := strings.ToLower(chi.URLParam(r, "tokenAddress"))
	return token, addressRE.MatchString(token)
}

// metadata serves IF_METADATA_GET (MetadataGetResponse).
func (d *DevelopmentV2Reads) metadata(w http.ResponseWriter, r *http.Request) {
	token, ok := developmentToken(r)
	if !ok {
		v2Failure(w, ErrMissing)
		return
	}
	if published, err := d.publishedDocument(r.Context(), token); err != nil {
		developmentFailure(w, err)
		return
	} else if published {
		d.Connected.getMetadata(w, withTokenParam(r, token))
		return
	}
	row, err := d.poolRow(r.Context(), token)
	if err != nil {
		developmentFailure(w, err)
		return
	}
	data, _, err := d.legacyData(r.Context(), row)
	if err != nil {
		developmentFailure(w, err)
		return
	}
	resolved, err := d.chainToken(r.Context(), row)
	if err != nil {
		developmentFailure(w, err)
		return
	}
	mapped, err := V2ContractData(data, V2CanonicalIdentity{Token: resolved, PoolAddress: row.Pool, Creator: row.Creator})
	if err != nil {
		developmentFailure(w, err)
		return
	}
	d.respond(w, mapped, "no-store")
}

// media serves IF_CONNECTED_TOKEN_MEDIA (ConnectedTokenMediaResponse). A
// token whose metadata was published through /v3 metadata-publications is
// served by the production ConnectedMedia handler (image with its approved
// 256/512/1024 variants). Otherwise the legacy store holds no
// content-addressed approved variants, so image is null; the legacy
// /logo/{file} route keeps serving the bytes. Like production, a token
// without a metadata record is a JSON 404.
func (d *DevelopmentV2Reads) media(w http.ResponseWriter, r *http.Request) {
	token, ok := developmentToken(r)
	if !ok {
		v2Failure(w, ErrMissing)
		return
	}
	if published, err := d.publishedDocument(r.Context(), token); err != nil {
		developmentFailure(w, err)
		return
	} else if published {
		d.Connected.media(w, withTokenParam(r, token))
		return
	}
	row, err := d.poolRow(r.Context(), token)
	if err != nil {
		developmentFailure(w, err)
		return
	}
	data, found, err := d.legacyData(r.Context(), row)
	if err != nil {
		developmentFailure(w, err)
		return
	}
	if !found {
		v2Failure(w, ErrMissing)
		return
	}
	d.respond(w, ConnectedMetadata{Metadata: data}, "public, max-age=60")
}

func (d *DevelopmentV2Reads) respond(w http.ResponseWriter, data any, cache string) {
	response, err := envelope(uuid.NewString(), data, "partial", d.Clock())
	if err != nil {
		developmentFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cache)
	json.NewEncoder(w).Encode(response)
}

func developmentFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrChainUnavailable) {
		v2Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	v2Failure(w, err)
}
