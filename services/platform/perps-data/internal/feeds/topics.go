package feeds

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/bech32"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
)

// Fixed topics of the markets and chain families.
const (
	TopicMarketsAll    = "markets@all"
	TopicMarketsTotals = "markets@totals"
	TopicChainBlocks   = "chain@blocks"
	TopicChainTxs      = "chain@transactions"
)

// Symbol topic kinds, the part after <SYMBOL>@.
const (
	kindPerpBook  = "perp_orderbook"
	kindPerpTrade = "perp_trade"
	kindPerpStats = "perp_stats"
	kindMarket    = "market"
	kindBook      = "orderbook"
	kindTrade     = "trade"
	kindStats     = "stats"
)

// Snapshot depths and broadcast intervals (contracts/data-services.md
// sections 10, 11 and 16).
const (
	TradeSnapshot  = 100
	BlockSnapshot  = 20
	TxSnapshot     = 50
	BookInterval   = 100 * time.Millisecond
	ChecksumEvery  = 5 * time.Second
	StatsInterval  = 30 * time.Second
	MarketInterval = 5 * time.Second
)

// spotRank is the position of a spot token among the registered
// non-stablecoin tokens of 0x1025, which the activation registers as USDL,
// PAX, SID (contracts/spot.md, Opening pairs). Only these two symbols carry
// spot topics; the others stay on the Chainflow feed.
var spotRank = map[string]int{"PAX": 0, "SID": 1}

// perpsEmitter is the perpetuals precompile, whose tx_actions rows carry
// 18-decimal sizes.
var perpsEmitter = []byte{18: 0x10, 19: 0x15}

// countActions are the explorer actions whose size is a count, not an amount.
var countActions = map[string]bool{"Oracle Updated": true, "Sealed Batch Executed": true}

// ErrGap reports a topic whose live frames cannot be continued from the
// stream; the topic must send a fresh snapshot.
var ErrGap = errors.New("feeds: topic needs a fresh snapshot")

// Frame is one live frame of a topic.
type Frame struct {
	Topic string
	Data  json.RawMessage
}

type marks struct {
	Mark  *float64 `json:"mark_price"`
	Index *float64 `json:"index_price"`
	Basis *float64 `json:"basis_bps"`
}

type bookState struct {
	symbol string
	perp   *Book
	market string
	marks  marks
	token  []byte
	// spotAsks and spotBids are the levels of the last read of the open
	// spot orders, up to SnapshotLevels per side.
	spotAsks, spotBids []Level
	// snap is the encoded subscribe snapshot of the book as it stands, nil
	// once the book changed.
	snap  json.RawMessage
	dirty bool
	// lastFrame and lastChecksum are the times of the last live frame and
	// of the last frame that carried checksum.
	lastFrame, lastChecksum time.Time
}

// cursor follows a newest-first item list by the id of the newest item
// already sent; items is the list as last read, the snapshot of the tape.
type cursor struct {
	last  string
	items []item
	// snap is the encoded subscribe snapshot of items, nil once they changed.
	snap json.RawMessage
}

type item struct {
	id   string
	data json.RawMessage
}

// advance returns the items newer than the cursor, oldest first. The list
// holds the newest window items; when the last sent item is not in it the
// items in between are unknown and advance reports ErrGap.
func (c *cursor) advance(items []item, window int) ([]item, error) {
	prev := c.last
	i := len(items)
	if prev != "" {
		i = slices.IndexFunc(items, func(it item) bool { return it.id == prev })
	}
	if len(items) > 0 {
		c.last = items[0].id
	}
	if i < 0 || (prev == "" && len(items) >= window) {
		return nil, ErrGap
	}
	out := slices.Clone(items[:i])
	slices.Reverse(out)
	return out, nil
}

// Providers serves the book, trade, stats, market, totals and chain topics
// from schema perps. The hub calls Subscribed before a subscribe and
// Unsubscribed when a subscriber leaves, feeds every stream message to
// Message in stream order and runs Run for the timed frames. A subscribed
// topic holds its state: Load reads it from the database once, Message and
// the timed frames keep it current, and Held answers a subscribe snapshot
// from it without IO. State is kept only for topics with a subscriber.
type Providers struct {
	db     *sql.DB
	symbol *records.Symbol
	logger *slog.Logger
	// symbols maps every market symbol to its decimal market id.
	symbols map[string]string
	ids     map[string]string
	order   []string

	// mu guards the held state; Message and the book frames read the
	// database under it, Load and Snapshot read outside it.
	mu      sync.Mutex
	watched map[string]int
	books   map[string]*bookState
	cursors map[string]*cursor
	// bodies holds the last body of a subscribed stats, market, markets@all
	// or markets@totals topic.
	bodies map[string]json.RawMessage
	// loading holds the Load in flight of a topic.
	loading map[string]*load
	// pre holds the reads Prepare made for the messages not applied yet, by
	// event id; cur is the one of the Message that runs.
	pre map[string]*prefetch
	cur *prefetch

	tokenMu    sync.Mutex
	spotTokens map[string][]byte

	lastStats, lastMarkets time.Time
}

const marketsSQL = `SELECT market_id::text, symbol FROM perps.markets ORDER BY market_id`

// NewProviders reads the market set of perps.markets and serves its topics,
// reading the symbol bodies of records.Symbol for the stats and trade payloads.
func NewProviders(ctx context.Context, db *sql.DB, logger *slog.Logger) (*Providers, error) {
	symbol, err := records.NewSymbol(db)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	p := &Providers{
		db: db, symbol: symbol, logger: logger,
		symbols: map[string]string{}, ids: map[string]string{},
		watched: map[string]int{}, books: map[string]*bookState{}, cursors: map[string]*cursor{}, bodies: map[string]json.RawMessage{},
		loading: map[string]*load{}, pre: map[string]*prefetch{}, spotTokens: map[string][]byte{},
	}
	rows, err := db.QueryContext(ctx, marketsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sym string
		if err := rows.Scan(&id, &sym); err != nil {
			return nil, err
		}
		p.symbols[sym], p.ids[id] = id, sym
		p.order = append(p.order, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(p.order) == 0 {
		return nil, errors.New("feeds: perps.markets is empty")
	}
	return p, nil
}

// parse splits a served symbol topic into its symbol and kind.
func (p *Providers) parse(topic string) (string, string, bool) {
	sym, kind, ok := strings.Cut(topic, "@")
	if !ok {
		return "", "", false
	}
	if _, known := p.symbols[sym]; !known {
		return "", "", false
	}
	switch kind {
	case kindPerpBook, kindPerpTrade, kindPerpStats, kindMarket:
		return sym, kind, true
	case kindBook, kindTrade, kindStats:
		_, spot := spotRank[sym]
		return sym, kind, spot
	}
	return "", "", false
}

// Served reports whether topic is served: the perpetual topics and @market
// of every market, the spot topics of PAX and SID, and the fixed markets and
// chain topics. No kline topic exists; candles stay on the Chainflow feed
// (contracts/data-services.md section 12).
func (p *Providers) Served(topic string) bool {
	switch topic {
	case TopicMarketsAll, TopicMarketsTotals, TopicChainBlocks, TopicChainTxs:
		return true
	}
	_, _, ok := p.parse(topic)
	return ok
}

// SocketTopics returns the topics a socket opened on path is subscribed to
// on open: /ws/<symbol> the symbol topics of section 10, /ws/markets
// markets@all and markets@totals, /ws/explorer chain@blocks and
// chain@transactions. Other paths of the protocol subscribe to nothing; an
// unknown symbol path is not served.
func (p *Providers) SocketTopics(path string) ([]string, bool) {
	switch path {
	case "/", "/ws":
		return nil, true
	case "/ws/markets":
		return []string{TopicMarketsAll, TopicMarketsTotals}, true
	case "/ws/explorer":
		return []string{TopicChainBlocks, TopicChainTxs}, true
	}
	lower, ok := strings.CutPrefix(path, "/ws/")
	sym := strings.ToUpper(lower)
	if _, known := p.symbols[sym]; !ok || !known || lower != strings.ToLower(lower) {
		return nil, false
	}
	out := []string{sym + "@" + kindPerpBook, sym + "@" + kindPerpTrade, sym + "@" + kindPerpStats}
	if _, spot := spotRank[sym]; spot {
		out = append(out, sym+"@"+kindBook, sym+"@"+kindTrade, sym+"@"+kindStats)
	}
	return out, true
}

// Subscribed counts a subscriber of topic.
func (p *Providers) Subscribed(topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watched[topic]++
}

// Unsubscribed drops a subscriber of topic and the topic state with the
// last one.
func (p *Providers) Unsubscribed(topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watched[topic] <= 1 {
		delete(p.watched, topic)
		delete(p.books, topic)
		delete(p.cursors, topic)
		delete(p.bodies, topic)
		return
	}
	p.watched[topic]--
}

// held is the state of one topic: the book of a book topic, the cursor of a
// tape, else the body.
type held struct {
	book *bookState
	tape *cursor
	body json.RawMessage
}

func (h held) empty() bool { return h.book == nil && h.tape == nil && h.body == nil }

// load is a Load in flight; stale is set by a message of its topic, or a
// rewind, applied while it reads.
type load struct {
	done  chan struct{}
	err   error
	stale bool
}

// build reads the state of topic from the database.
func (p *Providers) build(ctx context.Context, topic string) (held, error) {
	switch topic {
	case TopicMarketsAll, TopicMarketsTotals:
		recs, err := p.records(ctx, p.order)
		if err != nil {
			return held{}, err
		}
		var v any = recs
		if topic == TopicMarketsTotals {
			v = totalsOf(recs)
		}
		body, err := json.Marshal(v)
		return held{body: body}, err
	case TopicChainBlocks, TopicChainTxs:
		items, err := p.chainItems(ctx, topic)
		if err != nil {
			return held{}, err
		}
		return held{tape: tapeOf(items)}, nil
	}
	sym, kind, ok := p.parse(topic)
	if !ok {
		return held{}, fmt.Errorf("feeds: topic %q is not served", topic)
	}
	switch kind {
	case kindStats, kindPerpStats:
		body, err := p.stats(ctx, sym, kind)
		return held{body: body}, err
	case kindMarket:
		recs, err := p.records(ctx, []string{sym})
		if err != nil {
			return held{}, err
		}
		body, err := json.Marshal(recs[0])
		return held{body: body}, err
	case kindTrade, kindPerpTrade:
		items, err := p.trades(ctx, sym, kind)
		if err != nil {
			return held{}, err
		}
		return held{tape: tapeOf(items)}, nil
	}
	b, err := p.loadBook(ctx, sym, kind)
	if err != nil {
		return held{}, err
	}
	if b.token != nil {
		if b.spotAsks, b.spotBids, err = spotLevels(ctx, p.db, b.token, SnapshotLevels); err != nil {
			return held{}, err
		}
	}
	return held{book: b}, nil
}

func tapeOf(items []item) *cursor {
	c := &cursor{items: items}
	if len(items) > 0 {
		c.last = items[0].id
	}
	return c
}

func (p *Providers) heldLocked(topic string) held {
	return held{book: p.books[topic], tape: p.cursors[topic], body: p.bodies[topic]}
}

// installLocked makes h the state of topic; a book starts its live frames
// and checksums afresh.
func (p *Providers) installLocked(topic string, h held) {
	delete(p.books, topic)
	delete(p.cursors, topic)
	delete(p.bodies, topic)
	switch {
	case h.book != nil:
		h.book.lastFrame, h.book.lastChecksum = time.Time{}, time.Time{}
		p.books[topic] = h.book
	case h.tape != nil:
		p.cursors[topic] = h.tape
	default:
		p.bodies[topic] = h.body
	}
}

// touchLocked marks the Load in flight of topic stale: a message of the
// topic was applied while it read, so its state may miss that message.
func (p *Providers) touchLocked(topic string) {
	if l := p.loading[topic]; l != nil {
		l.stale = true
	}
}

// encode writes the snapshot of topic from h without IO.
// The encoded snapshot of a book or tape is kept until its state changes.
func encode(topic string, h held) (json.RawMessage, error) {
	switch {
	case h.book != nil:
		if h.book.snap == nil {
			data, err := bookBody(h.book, SnapshotLevels, false)
			if err != nil {
				return nil, err
			}
			h.book.snap = data
		}
		return h.book.snap, nil
	case h.tape == nil:
		return h.body, nil
	}
	if h.tape.snap != nil {
		return h.tape.snap, nil
	}
	data := make([]json.RawMessage, len(h.tape.items))
	for i, it := range h.tape.items {
		data[i] = it.data
	}
	var (
		body json.RawMessage
		err  error
	)
	if topic == TopicChainBlocks || topic == TopicChainTxs {
		body, err = json.Marshal(data)
	} else {
		sym, _, _ := strings.Cut(topic, "@")
		body, err = json.Marshal(struct {
			Symbol string            `json:"symbol"`
			Trades []json.RawMessage `json:"trades"`
		}{sym, data})
	}
	if err != nil {
		return nil, err
	}
	h.tape.snap = body
	return body, nil
}

// Load reads the state of a subscribed topic that holds none, outside the
// lock Message takes, so a subscribe never stalls the stream. One Load per
// topic reads at a time; the others wait for it. A state that a message of
// its topic may have missed while it was read is discarded, and Held stays
// false.
func (p *Providers) Load(ctx context.Context, topic string) error {
	p.mu.Lock()
	if p.watched[topic] == 0 || !p.heldLocked(topic).empty() {
		p.mu.Unlock()
		return nil
	}
	if l := p.loading[topic]; l != nil {
		p.mu.Unlock()
		select {
		case <-l.done:
			return l.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l := &load{done: make(chan struct{})}
	p.loading[topic] = l
	p.mu.Unlock()
	h, err := p.build(ctx, topic)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.loading, topic)
	l.err = err
	close(l.done)
	if err == nil && !l.stale && p.watched[topic] > 0 && p.heldLocked(topic).empty() {
		p.installLocked(topic, h)
	}
	return err
}

// Held returns the subscribe snapshot of topic from the state it holds,
// without IO. It reports false while the topic holds no state.
func (p *Providers) Held(topic string) (json.RawMessage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.heldLocked(topic)
	if h.empty() {
		return nil, false
	}
	data, err := encode(topic, h)
	if err != nil {
		p.logger.Error("feeds: held snapshot", slog.String("topic", topic), slog.Any("err", err))
		return nil, false
	}
	return data, true
}

// Snapshot reads the snapshot of topic from the database. A subscribed
// topic holds the state the snapshot was built from in place of the state it
// held, so its live frames continue from it.
func (p *Providers) Snapshot(ctx context.Context, topic string) (json.RawMessage, error) {
	h, err := p.build(ctx, topic)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watched[topic] > 0 {
		p.installLocked(topic, h)
	}
	return encode(topic, h)
}

// prefetch is what Prepare read for one message: the order rows of
// its source position by market, the newest items of its tape by topic and
// the marks of its market by symbol.
type prefetch struct {
	rows  map[string][]decode.Row
	items map[string][]item
	marks map[string]marks
}

// Prepare makes, outside the lock under which the hub applies messages, the
// database reads that Message of the same message makes for the state the
// topics hold now; Message uses them in place of its own. A read that fails
// here is left to Message. The hub prepares the messages of different
// families at once.
func (p *Providers) Prepare(ctx context.Context, subject string, env delivery.Envelope) {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != "perps" {
		return
	}
	pf := &prefetch{rows: map[string][]decode.Row{}, items: map[string][]item{}, marks: map[string]marks{}}
	var book, tape, sym string
	switch family := parts[2]; {
	case family == "chain" && len(parts) == 4 && parts[3] == "block":
		tape = TopicChainBlocks
	case family == "chain" && len(parts) == 4 && parts[3] == "tx":
		tape = TopicChainTxs
	case family == "spot" && len(parts) == 5 && (parts[4] == "order" || parts[4] == "swap"):
		spot, err := p.spotSymbol(ctx, parts[3])
		if err != nil || spot == "" {
			return
		}
		tape = spot + "@" + kindTrade
	case family != "chain" && family != "spot" && family != "user" && len(parts) == 4:
		var ok bool
		if sym, ok = p.ids[family]; !ok {
			return
		}
		book = sym + "@" + kindPerpBook
		if parts[3] == "fill" {
			tape = sym + "@" + kindPerpTrade
		}
	}
	p.mu.Lock()
	b, c := p.books[book], p.cursors[tape]
	p.mu.Unlock()
	// The book and tape reads are independent and run side by side.
	var reads sync.WaitGroup
	if c != nil {
		reads.Go(func() {
			if items, _, err := p.tapeItems(ctx, tape); err == nil {
				pf.items[tape] = items
			}
		})
	}
	if b != nil {
		switch parts[3] {
		case "order", "fill":
			if pos, err := sourcePos(env.SourceOrder); err == nil {
				if rows, err := loadOrderRows(ctx, p.db, b.market, pos[0], pos[1], pos[2]); err == nil {
					pf.rows[b.market] = rows
				}
			}
		case "mark", "index":
			if m, err := p.marks(ctx, sym); err == nil {
				pf.marks[sym] = m
			}
		}
	}
	reads.Wait()
	p.mu.Lock()
	p.pre[env.EventID] = pf
	p.mu.Unlock()
}

// Message maps one stream message to the live frames it causes and names the
// topics that must send a fresh snapshot: every watched topic after a
// rewind, and a tape whose next items cannot be read from the stored rows.
// Book changes are sent by Run, coalesced.
func (p *Providers) Message(ctx context.Context, subject string, env delivery.Envelope) ([]Frame, []string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != "perps" {
		return nil, nil, fmt.Errorf("feeds: subject %q outside perps.<chain>.>", subject)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cur = p.pre[env.EventID]
	delete(p.pre, env.EventID)
	defer func() { p.cur = nil }()
	switch family := parts[2]; {
	case family == "chain" && len(parts) == 4:
		switch parts[3] {
		case "block":
			return p.advance(ctx, TopicChainBlocks)
		case "tx":
			return p.advance(ctx, TopicChainTxs)
		case "rewind":
			resnap := make([]string, 0, len(p.watched))
			for topic := range p.watched {
				resnap = append(resnap, topic)
			}
			slices.Sort(resnap)
			p.books, p.cursors, p.bodies = map[string]*bookState{}, map[string]*cursor{}, map[string]json.RawMessage{}
			for _, l := range p.loading {
				l.stale = true
			}
			return nil, resnap, nil
		}
	case family == "spot" && len(parts) == 5:
		sym, err := p.spotSymbol(ctx, parts[3])
		if err != nil || sym == "" {
			return nil, nil, err
		}
		if parts[4] == "order" {
			p.touchLocked(sym + "@" + kindBook)
			if b, ok := p.books[sym+"@"+kindBook]; ok {
				b.dirty = true
			}
		}
		if parts[4] == "order" || parts[4] == "swap" {
			return p.advance(ctx, sym+"@"+kindTrade)
		}
	case family == "user":
	case len(parts) == 4:
		sym, ok := p.ids[family]
		if !ok {
			return nil, nil, nil
		}
		topic := sym + "@" + kindPerpBook
		p.touchLocked(topic)
		b := p.books[topic]
		switch parts[3] {
		case "order", "fill":
			if b != nil {
				if err := p.applyAt(ctx, b, env.SourceOrder); err != nil {
					return nil, nil, err
				}
			}
			if parts[3] == "fill" {
				return p.advance(ctx, sym+"@"+kindPerpTrade)
			}
		case "mark", "index":
			if b != nil {
				m, ok := marks{}, false
				if p.cur != nil {
					m, ok = p.cur.marks[sym]
				}
				if !ok {
					var err error
					if m, err = p.marks(ctx, sym); err != nil {
						return nil, nil, err
					}
				}
				b.marks, b.dirty, b.snap = m, true, nil
			}
		}
	}
	return nil, nil, nil
}

func (p *Providers) applyAt(ctx context.Context, b *bookState, at delivery.SourceOrder) error {
	rows, ok := []decode.Row(nil), false
	if p.cur != nil {
		rows, ok = p.cur.rows[b.market]
	}
	if !ok {
		pos, err := sourcePos(at)
		if err != nil {
			return err
		}
		if rows, err = loadOrderRows(ctx, p.db, b.market, pos[0], pos[1], pos[2]); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if err := b.perp.Apply(r); err != nil {
			return err
		}
	}
	if len(rows) > 0 {
		b.dirty, b.snap = true, nil
	}
	return nil
}

// sourcePos parses the height, transaction index and log index of at.
func sourcePos(at delivery.SourceOrder) ([3]int64, error) {
	var pos [3]int64
	for i, v := range []string{at.Height, at.TransactionIndex, at.LogIndex} {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return pos, fmt.Errorf("feeds: source order %+v: %w", at, err)
		}
		pos[i] = n
	}
	return pos, nil
}

// tapeWindow is the window the cursor of a tape keeps.
func tapeWindow(topic string) int {
	switch topic {
	case TopicChainBlocks:
		return BlockSnapshot
	case TopicChainTxs:
		return TxSnapshot
	}
	return TradeSnapshot
}

// tapeItems reads the newest items of a tape and the window its cursor keeps.
func (p *Providers) tapeItems(ctx context.Context, topic string) ([]item, int, error) {
	if topic == TopicChainBlocks || topic == TopicChainTxs {
		items, err := p.chainItems(ctx, topic)
		return items, tapeWindow(topic), err
	}
	sym, kind, _ := p.parse(topic)
	items, err := p.trades(ctx, sym, kind)
	return items, TradeSnapshot, err
}

// advance sends the items of a watched tape that are newer than its cursor.
func (p *Providers) advance(ctx context.Context, topic string) ([]Frame, []string, error) {
	p.touchLocked(topic)
	c, ok := p.cursors[topic]
	if !ok {
		return nil, nil, nil
	}
	var (
		items  []item
		window int
		err    error
		pre    bool
	)
	if p.cur != nil {
		items, pre = p.cur.items[topic]
	}
	if pre {
		window = tapeWindow(topic)
	} else {
		items, window, err = p.tapeItems(ctx, topic)
	}
	if err != nil {
		return nil, nil, err
	}
	fresh, err := c.advance(items, window)
	if errors.Is(err, ErrGap) {
		return nil, []string{topic}, nil
	}
	c.items, c.snap = items, nil
	frames := make([]Frame, len(fresh))
	for i, it := range fresh {
		frames[i] = Frame{Topic: topic, Data: it.data}
	}
	return frames, nil, nil
}

// trades reads the newest 100 trades of the tape from the symbol bodies,
// each with symbol added, newest first.
func (p *Providers) trades(ctx context.Context, sym, kind string) ([]item, error) {
	var (
		body any
		err  error
	)
	if kind == kindPerpTrade {
		body, err = p.symbol.PerpTrades(ctx, sym, TradeSnapshot)
	} else {
		body, err = p.symbol.Trades(ctx, sym, TradeSnapshot)
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var tape struct {
		Trades []map[string]json.RawMessage `json:"trades"`
	}
	if err := json.Unmarshal(raw, &tape); err != nil {
		return nil, err
	}
	symbol, err := json.Marshal(sym)
	if err != nil {
		return nil, err
	}
	out := make([]item, len(tape.Trades))
	for i, t := range tape.Trades {
		var id string
		if err := json.Unmarshal(t["id"], &id); err != nil {
			return nil, fmt.Errorf("feeds: trade without id: %w", err)
		}
		t["symbol"] = symbol
		data, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		out[i] = item{id: id, data: data}
	}
	return out, nil
}

// stats writes the @stats or @perp_stats body of sym.
func (p *Providers) stats(ctx context.Context, sym, kind string) (json.RawMessage, error) {
	if kind == kindStats {
		s, err := p.symbol.Stats(ctx, sym)
		if err != nil {
			return nil, err
		}
		return json.Marshal(s)
	}
	s, err := p.symbol.PerpStats(ctx, sym)
	if err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

func (p *Providers) marks(ctx context.Context, sym string) (marks, error) {
	s, err := p.symbol.PerpStats(ctx, sym)
	if err != nil {
		return marks{}, err
	}
	return marks{Mark: s.MarkPrice, Index: s.IndexPrice, Basis: s.BasisBps}, nil
}

const spotTokenSQL = `SELECT token FROM perps.spot_token_registry
	WHERE kind = 'token_registered' AND is_stablecoin = false
	ORDER BY block, tx_index, log_index, sub OFFSET $1 LIMIT 1`

// spotToken resolves the token of a spot symbol; nil while unregistered.
func (p *Providers) spotToken(ctx context.Context, sym string) ([]byte, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if t, ok := p.spotTokens[sym]; ok {
		return t, nil
	}
	var t []byte
	err := p.db.QueryRowContext(ctx, spotTokenSQL, spotRank[sym]).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.spotTokens[sym] = t
	return t, nil
}

// spotSymbol names the spot symbol of a lower-case 0x token subject part,
// or "" for a token without spot topics.
func (p *Providers) spotSymbol(ctx context.Context, token string) (string, error) {
	for _, sym := range []string{"PAX", "SID"} {
		t, err := p.spotToken(ctx, sym)
		if err != nil {
			return "", err
		}
		if t != nil && "0x"+hex.EncodeToString(t) == token {
			return sym, nil
		}
	}
	return "", nil
}

func (p *Providers) loadBook(ctx context.Context, sym, kind string) (*bookState, error) {
	b := &bookState{symbol: sym, market: p.symbols[sym]}
	if kind == kindBook {
		token, err := p.spotToken(ctx, sym)
		if err != nil {
			return nil, err
		}
		b.token = token
		return b, nil
	}
	book, err := LoadBook(ctx, p.db, b.market)
	if err != nil {
		return nil, err
	}
	b.perp = book
	if b.marks, err = p.marks(ctx, sym); err != nil {
		return nil, err
	}
	return b, nil
}

type bookFrame struct {
	Symbol string `json:"symbol"`
	*marks
	Asks     []Level `json:"asks"`
	Bids     []Level `json:"bids"`
	Checksum *uint32 `json:"checksum,omitempty"`
}

// bookBody writes depth levels per side of the held book, with the CRC32 of
// its top 20 levels when withChecksum is set.
func bookBody(b *bookState, depth int, withChecksum bool) (json.RawMessage, error) {
	f := bookFrame{Symbol: b.symbol, Asks: []Level{}, Bids: []Level{}}
	asks, bids := b.spotAsks, b.spotBids
	if b.perp != nil {
		asks, bids = b.perp.Levels(SnapshotLevels)
		f.marks = &b.marks
	}
	f.Asks = append(f.Asks, asks[:min(len(asks), depth)]...)
	f.Bids = append(f.Bids, bids[:min(len(bids), depth)]...)
	if withChecksum {
		sum := checksum(asks, bids)
		f.Checksum = &sum
	}
	return json.Marshal(f)
}

// Run sends the timed frames until ctx ends: book frames at most 10 per
// second per topic, the top 20 levels, with checksum at least every 5
// seconds; stats every 30 seconds; markets@all, markets@totals and every
// watched <SYMBOL>@market every 5 seconds. The stats and market bodies are
// read in a loop of their own, so their reads never delay a book frame.
func (p *Providers) Run(ctx context.Context, emit func(Frame)) error {
	var refresh sync.WaitGroup
	defer refresh.Wait()
	refresh.Go(func() { p.every(ctx, emit, p.bodyFrames) })
	p.every(ctx, emit, p.bookFrames)
	return ctx.Err()
}

// every emits the frames of next at every BookInterval until ctx ends.
func (p *Providers) every(ctx context.Context, emit func(Frame), next func(context.Context, time.Time) []Frame) {
	t := time.NewTicker(BookInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, f := range next(ctx, now) {
				emit(f)
			}
		}
	}
}

func (p *Providers) tick(ctx context.Context, now time.Time) []Frame {
	return append(p.bookFrames(ctx, now), p.bodyFrames(ctx, now)...)
}

// bodyFrames reads the stats bodies every 30 seconds and the market bodies
// every 5 seconds, and holds those of the subscribed topics.
func (p *Providers) bodyFrames(ctx context.Context, now time.Time) []Frame {
	var bodies []Frame
	p.mu.Lock()
	stats := now.Sub(p.lastStats) >= StatsInterval
	markets := now.Sub(p.lastMarkets) >= MarketInterval
	if stats {
		p.lastStats = now
	}
	var watchedMarkets []string
	if markets {
		p.lastMarkets = now
		for _, sym := range p.order {
			if p.watched[sym+"@"+kindMarket] > 0 {
				watchedMarkets = append(watchedMarkets, sym)
			}
		}
	}
	p.mu.Unlock()
	if stats {
		for _, sym := range p.order {
			kinds := []string{kindPerpStats}
			if _, spot := spotRank[sym]; spot {
				kinds = append(kinds, kindStats)
			}
			for _, kind := range kinds {
				data, err := p.stats(ctx, sym, kind)
				if err != nil {
					p.logger.Error("feeds: stats frame", slog.String("symbol", sym), slog.Any("err", err))
					continue
				}
				bodies = append(bodies, Frame{Topic: sym + "@" + kind, Data: data})
			}
		}
	}
	if markets {
		bodies = append(bodies, p.marketFrames(ctx, watchedMarkets)...)
	}
	p.mu.Lock()
	for _, f := range bodies {
		if p.watched[f.Topic] > 0 {
			p.bodies[f.Topic] = f.Data
		}
	}
	p.mu.Unlock()
	return bodies
}

func (p *Providers) bookFrames(ctx context.Context, now time.Time) []Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	topics := make([]string, 0, len(p.books))
	for topic := range p.books {
		topics = append(topics, topic)
	}
	slices.Sort(topics)
	var frames []Frame
	for _, topic := range topics {
		b := p.books[topic]
		withChecksum := now.Sub(b.lastChecksum) >= ChecksumEvery
		if !withChecksum && (!b.dirty || now.Sub(b.lastFrame) < BookInterval) {
			continue
		}
		if b.token != nil && b.dirty {
			asks, bids, err := spotLevels(ctx, p.db, b.token, SnapshotLevels)
			if err != nil {
				p.logger.Error("feeds: book frame", slog.String("topic", topic), slog.Any("err", err))
				continue
			}
			b.spotAsks, b.spotBids, b.snap = asks, bids, nil
		}
		data, err := bookBody(b, LiveLevels, withChecksum)
		if err != nil {
			p.logger.Error("feeds: book frame", slog.String("topic", topic), slog.Any("err", err))
			continue
		}
		frames = append(frames, Frame{Topic: topic, Data: data})
		b.dirty, b.lastFrame = false, now
		if withChecksum {
			b.lastChecksum = now
		}
	}
	return frames
}

func (p *Providers) marketFrames(ctx context.Context, watched []string) []Frame {
	recs, err := p.records(ctx, p.order)
	if err != nil {
		p.logger.Error("feeds: market frames", slog.Any("err", err))
		return nil
	}
	all, err := json.Marshal(recs)
	if err != nil {
		p.logger.Error("feeds: market frames", slog.Any("err", err))
		return nil
	}
	totals, err := json.Marshal(totalsOf(recs))
	if err != nil {
		p.logger.Error("feeds: market frames", slog.Any("err", err))
		return nil
	}
	frames := []Frame{{Topic: TopicMarketsAll, Data: all}, {Topic: TopicMarketsTotals, Data: totals}}
	for _, r := range recs {
		if !slices.Contains(watched, r.Symbol) {
			continue
		}
		data, err := json.Marshal(r)
		if err != nil {
			p.logger.Error("feeds: market frame", slog.String("symbol", r.Symbol), slog.Any("err", err))
			continue
		}
		frames = append(frames, Frame{Topic: r.Symbol + "@" + kindMarket, Data: data})
	}
	return frames
}

// totalsOf sums the platform totals over the market records.
func totalsOf(recs []records.MarketRecord) records.Totals {
	var t records.Totals
	for _, r := range recs {
		t.TotalTrades += r.TradesCount
		t.Volume24h += r.Volume24h
		t.Volume7d += r.Volume7d
		t.Volume30d += r.Volume30d
		t.TotalOI += r.OpenInterest
	}
	return t
}

// OIContractsSQL is the open interest in contracts of market m.market_id: the
// base size of its open positions, size_usd (the cost basis) over entry_price
// (the base-weighted average entry the chain holds) per position.
const OIContractsSQL = `(SELECT COALESCE(sum(p.size_usd / p.entry_price), 0) FROM perps.positions_current p
	WHERE p.market_id = m.market_id AND p.status = 'open' AND p.entry_price > 0)`

// rollingSQL reads the rolling market statistics, the 24-hour change of the
// last fill price, the time of the block that last updated them, the 24-hour
// fill volume in contracts and the open interest in contracts.
const rollingSQL = `SELECT m.symbol, m.name, s.volume_7d / $1::numeric, s.volume_30d / $1::numeric, s.trades_count,
	(s.last_price - s.open_24h) / NULLIF(s.open_24h, 0), b.time,
	(SELECT COALESCE(sum(f.size_usd / NULLIF(f.price, 0)), 0) FROM perps.fills f
		WHERE f.market_id = m.market_id AND f.block_time >= $2), ` + OIContractsSQL + `
	FROM perps.markets m
	LEFT JOIN perps.market_stats_rolling s ON s.market_id = m.market_id
	LEFT JOIN perps.blocks b ON b.number = s.updated_block
	ORDER BY m.market_id`

// records builds the market records of syms in market id order: the
// /perp_stats fields of the symbol routes, the rolling statistics and the
// next whole UTC funding hour. funding_8h is 8 times the hourly rate of the
// latest FundingRateUpdated.
func (p *Providers) records(ctx context.Context, syms []string) ([]records.MarketRecord, error) {
	now := time.Now()
	rows, err := p.db.QueryContext(ctx, rollingSQL, usdScale.String(), now.Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []records.MarketRecord
	for rows.Next() {
		var (
			r                                records.MarketRecord
			vol7, vol30, change, volContract sql.NullString
			oiContracts                      sql.NullString
			trades                           sql.NullInt64
			updated                          sql.NullTime
		)
		if err := rows.Scan(&r.Symbol, &r.SymbolFull, &vol7, &vol30, &trades, &change, &updated, &volContract,
			&oiContracts); err != nil {
			return nil, err
		}
		if !slices.Contains(syms, r.Symbol) {
			continue
		}
		if r.Change24h, err = optFloat(change); err != nil {
			return nil, err
		}
		if r.PerpOIContracts, err = optFloat(oiContracts); err != nil {
			return nil, err
		}
		for _, f := range []struct {
			raw sql.NullString
			dst *float64
		}{{vol7, &r.Volume7d}, {vol30, &r.Volume30d}, {volContract, &r.PerpVolume24hContracts}} {
			v, err := optFloat(f.raw)
			if err != nil {
				return nil, err
			}
			if v != nil {
				*f.dst = *v
			}
		}
		r.TradesCount = trades.Int64
		if updated.Valid {
			ms := updated.Time.UnixMilli()
			r.LastUpdated = &ms
		}
		r.PerpNextFundingMs = now.UTC().Truncate(time.Hour).Add(time.Hour).UnixMilli()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) != len(syms) {
		return nil, fmt.Errorf("feeds: %d of %d markets found", len(out), len(syms))
	}
	for i := range out {
		r := &out[i]
		s, err := p.symbol.PerpStats(ctx, r.Symbol)
		if err != nil {
			return nil, err
		}
		r.Price, r.Volume24h, r.OpenInterest, r.EstFundingRate = s.IndexPrice, s.Volume24h, s.OpenInterest, s.EstFundingRate
		if s.EstFundingRate != nil {
			f8 := 8 * *s.EstFundingRate
			r.Funding8h = &f8
		}
		r.PerpMarkPrice, r.PerpIndexPrice, r.PerpBasisBps = s.MarkPrice, s.IndexPrice, s.BasisBps
		r.PerpFundingRate, r.PerpLongShortRatio, r.PerpLiqVolume24h = s.LastFundingRate, s.LongShortRatio, s.LiqVolume24h
		r.PerpOIUSD, r.PerpVolume24hUSD = s.OpenInterest, s.Volume24h
	}
	return out, nil
}

func optFloat(raw sql.NullString) (*float64, error) {
	if !raw.Valid {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw.String, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

const (
	blocksSQL = `SELECT number, time, proposer, hash, tx_count FROM perps.blocks ORDER BY number DESC LIMIT $1`
	txsSQL    = `SELECT block, tx_index, log_index, sub, tx_hash, action, block_time, account, symbol, side,
		price::text, size::text, emitter
	FROM perps.tx_actions ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT $1`
	isoMillis = "2006-01-02T15:04:05.000Z"
)

type blockItem struct {
	Number       int64  `json:"block_number"`
	Time         string `json:"time"`
	Proposer     string `json:"proposer"`
	Hash         string `json:"hash"`
	Transactions int64  `json:"transactions"`
}

type txItem struct {
	Hash   string   `json:"hash"`
	Action string   `json:"action"`
	Block  int64    `json:"block"`
	Time   string   `json:"time"`
	User   string   `json:"user"`
	Symbol *string  `json:"symbol"`
	Side   *string  `json:"side"`
	Price  *float64 `json:"price"`
	Size   *float64 `json:"size"`
}

// chainItems reads the newest 20 blocks or 50 explorer rows, newest first.
// user is the bech32 form with prefix pax of the row account; prices are
// 18-decimal, sizes 18-decimal on perps rows, 6-decimal on spot rows and
// counts on the Oracle Updated and Sealed Batch Executed rows.
func (p *Providers) chainItems(ctx context.Context, topic string) ([]item, error) {
	if topic == TopicChainBlocks {
		rows, err := p.db.QueryContext(ctx, blocksSQL, BlockSnapshot)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []item
		for rows.Next() {
			var (
				b    blockItem
				at   time.Time
				hash []byte
			)
			if err := rows.Scan(&b.Number, &at, &b.Proposer, &hash, &b.Transactions); err != nil {
				return nil, err
			}
			b.Time, b.Hash = at.UTC().Format(isoMillis), "0x"+hex.EncodeToString(hash)
			data, err := json.Marshal(b)
			if err != nil {
				return nil, err
			}
			out = append(out, item{id: strconv.FormatInt(b.Number, 10), data: data})
		}
		return out, rows.Err()
	}
	rows, err := p.db.QueryContext(ctx, txsSQL, TxSnapshot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []item
	for rows.Next() {
		var (
			t                       txItem
			txIndex, logIndex, sub  int64
			at                      time.Time
			hash, account, emitter  []byte
			symbol, side, price, sz sql.NullString
		)
		if err := rows.Scan(&t.Block, &txIndex, &logIndex, &sub, &hash, &t.Action, &at, &account, &symbol, &side, &price, &sz, &emitter); err != nil {
			return nil, err
		}
		if t.User, err = bech32.ConvertAndEncode("pax", account); err != nil {
			return nil, err
		}
		t.Hash, t.Time = "0x"+hex.EncodeToString(hash), at.UTC().Format(isoMillis)
		if symbol.Valid {
			t.Symbol = &symbol.String
		}
		if side.Valid {
			t.Side = &side.String
		}
		if t.Price, err = scaled(price, usdScale); err != nil {
			return nil, err
		}
		sizeScale := spotScale
		switch {
		case countActions[t.Action]:
			sizeScale = big.NewInt(1)
		case slices.Equal(emitter, perpsEmitter):
			sizeScale = usdScale
		}
		if t.Size, err = scaled(sz, sizeScale); err != nil {
			return nil, err
		}
		data, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		id := fmt.Sprintf("%d:%d:%d:%d", t.Block, txIndex, logIndex, sub)
		out = append(out, item{id: id, data: data})
	}
	return out, rows.Err()
}

func scaled(raw sql.NullString, scale *big.Int) (*float64, error) {
	if !raw.Valid {
		return nil, nil
	}
	v, err := parseRaw(raw.String)
	if err != nil {
		return nil, err
	}
	f, _ := new(big.Rat).SetFrac(v, scale).Float64()
	return &f, nil
}
