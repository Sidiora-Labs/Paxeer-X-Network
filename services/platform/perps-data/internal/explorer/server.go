package explorer

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/bech32"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ratelimit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

const (
	cacheControl = "max-age=2"
	// headStaleAfter is how long the indexer head cursor may go without an
	// update before the chain counts as disconnected.
	headStaleAfter  = 30 * time.Second
	maxClientKeyLen = 512
	sweepInterval   = time.Minute
	unmatchedRoute  = "unmatched"
	errInternal     = "internal error"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{1,32}$`)

// Options configures a Server.
type Options struct {
	// LinkBase is PERPS_EXPLORER_TX_BASE; an empty base omits every link.
	LinkBase string
	// Requests is the number of requests one client key may make per Window.
	Requests int
	// Window is the fixed rate-limit window.
	Window time.Duration
	// Metrics counts every served request by route and status.
	Metrics *telemetry.Metrics
}

// Server serves the chain explorer REST routes of contracts/data-services.md
// section 16 from schema perps. It has no WebSocket of its own: chain@blocks
// and chain@transactions are served by the gateway socket, including
// /ws/explorer.
type Server struct {
	db      *sql.DB
	ex      *Explorer
	metrics *telemetry.Metrics
	limiter *windowLimiter
	mux     *http.ServeMux
}

// NewServer returns the explorer routes reading db.
func NewServer(db *sql.DB, opts Options) (*Server, error) {
	if opts.Metrics == nil {
		return nil, errors.New("explorer: nil metrics")
	}
	if opts.Requests <= 0 || opts.Window <= 0 {
		return nil, errors.New("explorer: rate limit needs positive requests and window")
	}
	ex, err := New(db, opts.LinkBase)
	if err != nil {
		return nil, err
	}
	s := &Server{
		db:      db,
		ex:      ex,
		metrics: opts.Metrics,
		limiter: &windowLimiter{max: opts.Requests, window: opts.Window, entries: map[string]windowEntry{}},
		mux:     http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /api/chain/blocks", s.blocks)
	s.mux.HandleFunc("GET /api/chain/transactions", s.transactions)
	s.mux.HandleFunc("GET /api/chain/health", s.chainHealth)
	s.mux.HandleFunc("GET /api/explorer/health", s.explorerHealth)
	s.mux.HandleFunc("GET /api/chain/accounts/{address}/history", s.accountHistory)
	s.mux.HandleFunc("GET /api/chain/markets/{symbol}/history", s.marketHistory)
	s.mux.HandleFunc("GET /api/chain/tx/{hash}", s.tx)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return s, nil
}

// ServeHTTP applies the per-client-key rate limit, serves the route and
// counts the answer under its route pattern.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	_, route := s.mux.Handler(r)
	if route == "" || route == "/" {
		route = unmatchedRoute
	}
	key := ratelimit.ClientKey(r)
	if len(key) > maxClientKeyLen {
		writeError(sw, http.StatusBadRequest, "invalid request principal")
	} else if ok, retry := s.limiter.allow(key, time.Now()); !ok {
		sw.Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(retry.Seconds())), 10))
		writeError(sw, http.StatusTooManyRequests, "rate limit exceeded")
	} else {
		s.mux.ServeHTTP(sw, r)
	}
	_ = s.metrics.RecordRead(route, sw.status)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) blocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ex.Blocks(r.Context(), r.URL.Query().Get("limit"))
	answer(w, map[string]any{"blocks": rows}, err)
}

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ex.Transactions(r.Context(), r.URL.Query().Get("limit"))
	answer(w, map[string]any{"transactions": rows}, err)
}

func (s *Server) accountHistory(w http.ResponseWriter, r *http.Request) {
	account, ok := parseAccount(r.PathValue("address"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid address")
		return
	}
	q := r.URL.Query()
	page, err := s.ex.AccountHistory(r.Context(), account, q.Get("cursor"), q.Get("limit"))
	answer(w, page, err)
}

func (s *Server) marketHistory(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	if !symbolPattern.MatchString(symbol) {
		writeError(w, http.StatusBadRequest, "invalid symbol")
		return
	}
	q := r.URL.Query()
	page, err := s.ex.MarketHistory(r.Context(), symbol, q.Get("cursor"), q.Get("limit"))
	answer(w, page, err)
}

// answer writes body, or maps a query error: an invalid limit or cursor is the
// caller's 400, anything else a 500.
func answer(w http.ResponseWriter, body any, err error) {
	switch {
	case errors.Is(err, ErrLimit):
		writeError(w, http.StatusBadRequest, "invalid limit")
	case errors.Is(err, ErrCursor):
		writeError(w, http.StatusBadRequest, "invalid cursor")
	case err != nil:
		writeError(w, http.StatusInternalServerError, errInternal)
	default:
		writeRows(w, body)
	}
}

const txQuery = `SELECT ` + actionColumns + ` FROM perps.tx_actions WHERE tx_hash = $1
    ORDER BY tx_index, log_index, sub`

// tx answers the stored tx_actions rows of a transaction or synthetic
// receipt; it never asks the node, so a synthetic receipt hash renders from
// the rows alone.
func (s *Server) tx(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("hash")
	b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if !strings.HasPrefix(raw, "0x") || err != nil || len(b) != common.HashLength {
		writeError(w, http.StatusBadRequest, "invalid transaction hash")
		return
	}
	hash := common.BytesToHash(b)
	rows, err := s.ex.readActions(r.Context(), txQuery, hash.Bytes())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "unknown transaction")
		return
	}
	txs := make([]Transaction, len(rows))
	for i, row := range rows {
		txs[i] = row.tx
	}
	body := struct {
		Hash         string        `json:"hash"`
		Link         string        `json:"link,omitempty"`
		Transactions []Transaction `json:"transactions"`
	}{hash.Hex(), s.ex.TxLink(hash), txs}
	writeRows(w, body)
}

type chainStatus struct {
	latest     sql.NullInt64
	blocksSeen int64
	txsSeen    int64
	connected  bool
}

// ponytail: count(*) scans both tables on every health read; switch to a
// maintained counter if the tables outgrow the 2 s cache.
const chainStatusSQL = `SELECT
  (SELECT max(number) FROM perps.blocks),
  (SELECT count(*) FROM perps.blocks),
  (SELECT count(*) FROM perps.tx_actions),
  COALESCE((SELECT updated_at > now() - make_interval(secs => $1) FROM perps.cursors WHERE name = 'head'), false)`

func (s *Server) status(ctx context.Context) (chainStatus, error) {
	var st chainStatus
	err := s.db.QueryRowContext(ctx, chainStatusSQL, headStaleAfter.Seconds()).Scan(&st.latest, &st.blocksSeen, &st.txsSeen, &st.connected)
	return st, err
}

func (s *Server) chainHealth(w http.ResponseWriter, r *http.Request) {
	st, err := s.status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	status := "ok"
	if !st.connected {
		status = "degraded"
	}
	writeRows(w, map[string]any{
		"status":          status,
		"ts":              time.Now().UnixMilli(),
		"chain_connected": st.connected,
		"blocks_seen":     st.blocksSeen,
		"txs_seen":        st.txsSeen,
	})
}

func (s *Server) explorerHealth(w http.ResponseWriter, r *http.Request) {
	st, err := s.status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	var latest *int64
	if st.latest.Valid {
		latest = &st.latest.Int64
	}
	writeRows(w, map[string]any{"latest_block": latest, "chain_connected": st.connected})
}

// parseAccount accepts the 0x hex or the pax bech32 form of an account.
func parseAccount(s string) (common.Address, bool) {
	if strings.HasPrefix(s, "0x") {
		if !common.IsHexAddress(s) {
			return common.Address{}, false
		}
		return common.HexToAddress(s), true
	}
	hrp, b, err := bech32.DecodeAndConvert(s)
	if err != nil || hrp != userPrefix || len(b) != common.AddressLength {
		return common.Address{}, false
	}
	return common.BytesToAddress(b), true
}

func writeRows(w http.ResponseWriter, body any) {
	w.Header().Set("Cache-Control", cacheControl)
	writeJSON(w, http.StatusOK, body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":"internal error"}`)
		w.Header().Del("Cache-Control")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(append(b, '\n'))
}

// windowLimiter is a per-client-key fixed-window request budget
// (Launch-x:shared/http/ratelimit.go in-memory store), one per process.
type windowLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]windowEntry
	swept   time.Time
}

type windowEntry struct {
	count   int
	resetAt time.Time
}

// allow spends one request of key's budget and reports whether it was
// available and, when not, how long until the window resets.
func (l *windowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > sweepInterval {
		for k, e := range l.entries {
			if !e.resetAt.After(now) {
				delete(l.entries, k)
			}
		}
		l.swept = now
	}
	e, ok := l.entries[key]
	if !ok || !e.resetAt.After(now) {
		e = windowEntry{resetAt: now.Add(l.window)}
	}
	if e.count >= l.max {
		return false, e.resetAt.Sub(now)
	}
	e.count++
	l.entries[key] = e
	return true, 0
}
