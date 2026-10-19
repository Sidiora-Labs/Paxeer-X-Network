// Package loadtest drives the load profile of the perps gateway sockets
// (contracts/data-services.md section 22): concurrent sockets on the book,
// trade and markets topics, a reconnect storm, and a Report that checks every
// frame with the sequence rule of the application, the frame delay, the book
// frame rate, depth and checksums, and the 1013 close reasons.
package loadtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/feeds"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ws"
)

// FeedSymbols are the eighteen symbols of the application feed list
// (Hyperpax:src/core/feeds.ts:4), in its order.
var FeedSymbols = []string{"BTC", "ETH", "SOL", "AVAX", "LINK", "HYPE", "XRP", "ASTER", "TRUMP", "BNB", "SID", "PAX", "TSLA", "NVDA", "GOOGL", "XAU", "NAS100", "SPX500"}

const (
	bookKind  = "@perp_orderbook"
	tradeKind = "@perp_trade"

	// maxDelayMillis bounds the delay histogram; a frame at or beyond it
	// counts in the last bucket.
	maxDelayMillis   = 60000
	handshakeTimeout = 10 * time.Second
	welcomeTimeout   = 10 * time.Second
	closeWait        = time.Second
	pollInterval     = 50 * time.Millisecond
	// closeRetryDefault and closeRetryMax are the back-off of the application
	// client after a close: 10 seconds for a 1013 without a hint, the hint
	// otherwise, at most 120 seconds (Hyperpax:src/core/net/realtime.ts:84-85).
	closeRetryDefault = 10 * time.Second
	closeRetryMax     = 120 * time.Second
	closeJitter       = time.Second
	retryAfterMin     = 5
	retryAfterMax     = 30
)

// retryAfter is the close reason of a 1013 (contracts/data-services.md
// section 8).
var retryAfter = regexp.MustCompile(`^retry-after=([0-9]{1,2})$`)

// Profile is the load profile of the gateway sockets.
type Profile struct {
	// Sockets is the number of concurrent sockets; socket i subscribes to
	// the book and trade topics of Symbols[i mod len(Symbols)] and to
	// markets@all.
	Sockets int      `json:"sockets"`
	Symbols []string `json:"symbols"`
	// StormFraction of the sockets drop at once and reconnect at most
	// DialRate per second.
	StormFraction float64 `json:"storm_fraction"`
	// DialRate bounds the dials per second of the ramp and the storm.
	DialRate int `json:"dial_rate"`
	// Recovery bounds the time from the first dial of the ramp, and from the
	// drop of the storm, until every socket is on a guarded live stream: a
	// welcome and a snapshot of each of its topics.
	Recovery time.Duration `json:"recovery_ns"`
	// Steady is how long every socket stays connected between the ramp and
	// the storm.
	Steady time.Duration `json:"steady_ns"`
	// BookFPS bounds the live frames of a book topic in any one second and
	// BookLevels the levels per side of a live book frame.
	BookFPS    int `json:"book_fps"`
	BookLevels int `json:"book_levels"`
	// ChecksumEvery is the longest interval between two checksum frames of a
	// book topic, measured on the frame times, beside MaxDelay.
	ChecksumEvery time.Duration `json:"checksum_every_ns"`
	// MaxDelay bounds the p99 delay from the frame ts to its receipt.
	MaxDelay time.Duration `json:"max_delay_ns"`
}

// FullProfile is the profile of contracts/data-services.md section 22:
// 140000 sockets across the gateway replicas.
func FullProfile() Profile {
	return Profile{
		Sockets:       140000,
		Symbols:       slices.Clone(FeedSymbols),
		StormFraction: 0.2,
		DialRate:      6000,
		Recovery:      60 * time.Second,
		Steady:        60 * time.Second,
		BookFPS:       10,
		BookLevels:    feeds.LiveLevels,
		ChecksumEvery: feeds.ChecksumEvery,
		MaxDelay:      250 * time.Millisecond,
	}
}

// Scaled returns the profile with n sockets.
func (p Profile) Scaled(n int) Profile {
	p.Sockets = n
	return p
}

// StormSockets is the number of sockets the storm drops.
func (p Profile) StormSockets() int { return int(math.Round(float64(p.Sockets) * p.StormFraction)) }

// Topics returns the topics of socket i.
func (p Profile) Topics(i int) []string {
	sym := p.Symbols[i%len(p.Symbols)]
	return []string{sym + bookKind, sym + tradeKind, feeds.TopicMarketsAll}
}

// Validate refuses a profile Run cannot drive.
func (p Profile) Validate() error {
	switch {
	case p.Sockets <= 0:
		return errors.New("loadtest: sockets must be positive")
	case len(p.Symbols) == 0:
		return errors.New("loadtest: no symbols")
	case p.StormFraction < 0 || p.StormFraction > 1 || math.IsNaN(p.StormFraction):
		return errors.New("loadtest: storm fraction outside [0, 1]")
	case p.DialRate <= 0:
		return errors.New("loadtest: dial rate must be positive")
	case p.Recovery <= 0, p.Steady <= 0, p.ChecksumEvery <= 0, p.MaxDelay <= 0:
		return errors.New("loadtest: durations must be positive")
	case p.BookFPS <= 0, p.BookLevels <= 0:
		return errors.New("loadtest: book bounds must be positive")
	}
	for _, s := range p.Symbols {
		if s == "" || strings.ContainsAny(s, "@ ") {
			return fmt.Errorf("loadtest: invalid symbol %q", s)
		}
	}
	return nil
}

// SocketURL returns the data socket URL /ws below a gateway base URL of
// scheme http, https, ws or wss.
func SocketURL(gateway string) (string, error) {
	u, err := url.Parse(gateway)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("loadtest: invalid gateway URL")
	}
	switch u.Scheme {
	case "http", "ws":
		u.Scheme = "ws"
	case "https", "wss":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("loadtest: gateway URL needs http, https, ws or wss")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/ws"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), nil
}

// Report is the outcome of one Run. Err names every violated rule.
type Report struct {
	Profile  Profile   `json:"profile"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	// Welcomed and Guarded count the sockets whose first connection received
	// the welcome, and a snapshot of every topic within Recovery of the
	// first dial.
	Welcomed int `json:"welcomed"`
	Guarded  int `json:"guarded"`
	// Storm sockets dropped at once; Recovered of them were back on a
	// guarded live stream within Recovery, the slowest after MaxRecovery.
	Storm       int           `json:"storm"`
	Recovered   int           `json:"recovered"`
	MaxRecovery time.Duration `json:"max_recovery_ns"`
	// Live counts the sockets on a guarded live stream when the run ended.
	Live int `json:"live"`

	Frames    int64 `json:"frames"`
	Snapshots int64 `json:"snapshots"`
	// Gaps are frames the sequence rule refused; each is followed by a
	// resubscribe, and Resynced counts the snapshots that resolved one.
	// UnresolvedGaps were still open when the run ended.
	Gaps           int64         `json:"gaps"`
	Resynced       int64         `json:"resynced"`
	UnresolvedGaps int64         `json:"unresolved_gaps"`
	P99Delay       time.Duration `json:"p99_delay_ns"`

	BookFrames          int64 `json:"book_frames"`
	BookRateViolations  int64 `json:"book_rate_violations"`
	BookLevelViolations int64 `json:"book_level_violations"`
	Checksums           int64 `json:"checksums"`
	ChecksumMismatches  int64 `json:"checksum_mismatches"`
	ChecksumLate        int64 `json:"checksum_late"`

	ErrorFrames       int64 `json:"error_frames"`
	ServerCloses      int64 `json:"server_closes"`
	Closes1013        int64 `json:"closes_1013"`
	Closes1013NoRetry int64 `json:"closes_1013_without_retry_after"`
	DialFailures      int64 `json:"dial_failures"`
}

// Err returns every rule of the profile the run violated, or nil.
func (r *Report) Err() error {
	p := r.Profile
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if r.Welcomed != p.Sockets {
		fail("%d of %d sockets received the welcome", r.Welcomed, p.Sockets)
	}
	if r.Guarded != p.Sockets {
		fail("%d of %d sockets reached a guarded live stream within %s", r.Guarded, p.Sockets, p.Recovery)
	}
	if r.Storm != p.StormSockets() {
		fail("storm dropped %d sockets, want %d", r.Storm, p.StormSockets())
	}
	if r.Recovered != r.Storm {
		fail("%d of %d storm sockets recovered within %s", r.Recovered, r.Storm, p.Recovery)
	}
	if r.MaxRecovery > p.Recovery {
		fail("slowest storm recovery %s above %s", r.MaxRecovery, p.Recovery)
	}
	if r.Live != p.Sockets {
		fail("%d of %d sockets live at the end", r.Live, p.Sockets)
	}
	if r.Frames == 0 {
		fail("no data frame")
	}
	if r.UnresolvedGaps != 0 {
		fail("%d sequence gaps not followed by a resync", r.UnresolvedGaps)
	}
	if r.P99Delay > p.MaxDelay {
		fail("p99 frame delay %s above %s", r.P99Delay, p.MaxDelay)
	}
	if r.Checksums == 0 {
		fail("no checksum frame")
	}
	if r.ChecksumMismatches != 0 {
		fail("%d checksums differ from the client book", r.ChecksumMismatches)
	}
	if r.ChecksumLate != 0 {
		fail("%d checksum intervals above %s", r.ChecksumLate, p.ChecksumEvery+p.MaxDelay)
	}
	if r.BookRateViolations != 0 {
		fail("%d book frames beyond %d per second", r.BookRateViolations, p.BookFPS)
	}
	if r.BookLevelViolations != 0 {
		fail("%d book frames with more than %d levels per side", r.BookLevelViolations, p.BookLevels)
	}
	if r.ErrorFrames != 0 {
		fail("%d error frames", r.ErrorFrames)
	}
	if r.Closes1013NoRetry != 0 {
		fail("%d 1013 closes without a retry-after reason", r.Closes1013NoRetry)
	}
	return errors.Join(errs...)
}

// Run drives p against the data socket at socketURL: it dials every socket
// at most p.DialRate per second and waits until each is on a guarded live
// stream, keeps them connected for p.Steady, drops p.StormSockets() at once,
// redials them and waits up to p.Recovery for them to recover, then closes
// every socket. It returns an error only for an invalid profile or URL or a
// cancelled ctx; the rules are checked by Report.Err.
func Run(ctx context.Context, socketURL string, p Profile) (*Report, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if u, err := url.Parse(socketURL); err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return nil, errors.New("loadtest: socket URL needs ws or wss")
	}
	r := &runner{
		profile: p,
		url:     socketURL,
		dialer:  &websocket.Dialer{HandshakeTimeout: handshakeTimeout},
		limiter: rate.NewLimiter(rate.Limit(p.DialRate), 1),
		report:  &Report{Profile: p, Started: time.Now()},
	}
	ctx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	r.sockets = make([]*socket, p.Sockets)
	for i := range r.sockets {
		s := &socket{run: r, topics: p.Topics(i), drop: make(chan struct{}, 1), books: map[string]*bookTrack{}, gaps: map[string]bool{}}
		r.sockets[i] = s
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.loop(ctx)
		}()
	}

	rampDeadline := r.report.Started.Add(p.Recovery)
	r.await(ctx, rampDeadline, func() bool { return int(r.guarded.Load()) == p.Sockets })
	if ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-time.After(p.Steady):
		}
	}

	storm := r.sockets[:p.StormSockets()]
	r.report.Storm = len(storm)
	stormAt := time.Now()
	r.stormAt.Store(stormAt.UnixNano())
	for _, s := range storm {
		s.drop <- struct{}{}
	}
	r.await(ctx, stormAt.Add(p.Recovery), func() bool { return int(r.recovered.Load()) == len(storm) })

	end := time.Now()
	r.endAt.Store(end.UnixNano())
	err := ctx.Err()
	stop()
	wg.Wait()
	r.finish(end)
	if err != nil {
		return r.report, err
	}
	return r.report, nil
}

type runner struct {
	profile Profile
	url     string
	dialer  *websocket.Dialer
	limiter *rate.Limiter
	sockets []*socket
	report  *Report

	// stormAt and endAt are Unix nanoseconds, zero until the storm and the
	// end of the run.
	stormAt, endAt atomic.Int64

	welcomed, guarded, recovered atomic.Int64
	maxRecovery                  atomic.Int64

	frames, snapshots, gaps, resynced                   atomic.Int64
	bookFrames, bookRate, bookLevels                    atomic.Int64
	checksums, mismatches, late                         atomic.Int64
	errorFrames, serverCloses, closes1013, noRetryAfter atomic.Int64
	dialFailures                                        atomic.Int64

	delays [maxDelayMillis + 1]atomic.Int64
}

// await polls done until it holds, deadline passes or ctx ends.
func (r *runner) await(ctx context.Context, deadline time.Time, done func() bool) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for !done() && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *runner) finish(end time.Time) {
	rep := r.report
	rep.Finished = end
	rep.Welcomed, rep.Guarded = int(r.welcomed.Load()), int(r.guarded.Load())
	rep.Recovered = int(r.recovered.Load())
	rep.MaxRecovery = time.Duration(r.maxRecovery.Load())
	rep.Frames, rep.Snapshots = r.frames.Load(), r.snapshots.Load()
	rep.Gaps, rep.Resynced = r.gaps.Load(), r.resynced.Load()
	rep.BookFrames, rep.BookRateViolations, rep.BookLevelViolations = r.bookFrames.Load(), r.bookRate.Load(), r.bookLevels.Load()
	rep.Checksums, rep.ChecksumMismatches, rep.ChecksumLate = r.checksums.Load(), r.mismatches.Load(), r.late.Load()
	rep.ErrorFrames, rep.ServerCloses = r.errorFrames.Load(), r.serverCloses.Load()
	rep.Closes1013, rep.Closes1013NoRetry = r.closes1013.Load(), r.noRetryAfter.Load()
	rep.DialFailures = r.dialFailures.Load()
	for _, s := range r.sockets {
		if s.liveAtEnd {
			rep.Live++
		}
		rep.UnresolvedGaps += int64(len(s.gaps))
	}
	rep.P99Delay = r.percentile(0.99)
}

// observe records the delay of one frame in whole milliseconds; a frame
// stamped after its receipt counts as zero.
func (r *runner) observe(delay time.Duration) {
	ms := min(max(delay.Milliseconds(), 0), maxDelayMillis)
	r.delays[ms].Add(1)
}

// percentile returns the smallest delay at or below which the fraction q of
// the frames arrived.
func (r *runner) percentile(q float64) time.Duration {
	var total int64
	for i := range r.delays {
		total += r.delays[i].Load()
	}
	if total == 0 {
		return 0
	}
	want := int64(math.Ceil(q * float64(total)))
	var seen int64
	for i := range r.delays {
		if seen += r.delays[i].Load(); seen >= want {
			return time.Duration(i) * time.Millisecond
		}
	}
	return maxDelayMillis * time.Millisecond
}

// frame is a server frame of the topic protocol: a control frame when Event
// is set, else a data frame.
type frame struct {
	Event string `json:"event"`
	ws.DataFrame
}

type bookData struct {
	Asks     []feeds.Level `json:"asks"`
	Bids     []feeds.Level `json:"bids"`
	Checksum *uint32       `json:"checksum"`
}

// bookTrack is the client book of one book topic: the levels of the last
// frame applied, the times of the recent live frames and the time of the last
// checksum or snapshot, in milliseconds of the frame ts and of the receipt.
type bookTrack struct {
	asks, bids   []feeds.Level
	recent       []int64
	lastChecksum int64
	lastRecv     time.Time
}

// socket is one simulated application client. Only its loop goroutine
// touches its state until Run's WaitGroup returns.
type socket struct {
	run    *runner
	topics []string
	drop   chan struct{}

	welcomed, guardedOnce, stormed bool
	liveAtEnd                      bool
	// gaps holds the topics with a refused frame and no snapshot since.
	gaps  map[string]bool
	books map[string]*bookTrack
}

type outcome int

const (
	// running is the zero outcome of a session that has not ended.
	running outcome = iota
	stopped
	dropped
	closedByServer
)

// loop connects and reconnects the socket until ctx ends. A dropped socket
// redials at once; a socket the server closed waits as the application
// client does: the retry-after hint, 10 seconds after a 1013 without one,
// else up to one second of jitter.
func (s *socket) loop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.run.limiter.Wait(ctx); err != nil {
			return
		}
		conn, resp, err := s.run.dialer.DialContext(ctx, s.run.url, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.run.dialFailures.Add(1)
			if !sleep(ctx, jitter(closeJitter)) {
				return
			}
			continue
		}
		out, wait := s.session(ctx, conn)
		switch out {
		case stopped:
			return
		case closedByServer:
			if !sleep(ctx, wait) {
				return
			}
		case dropped:
		}
	}
}

// session runs one connection until ctx ends, the storm drops it or the
// server closes it, and returns how it ended and, for a server close, how
// long the client waits before it redials.
func (s *socket) session(ctx context.Context, conn *websocket.Conn) (outcome, time.Duration) {
	var reason atomic.Int32 // an outcome
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			reason.CompareAndSwap(int32(running), int32(stopped))
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(closeWait))
		case <-s.drop:
			reason.CompareAndSwap(int32(running), int32(dropped))
		case <-done:
		}
		_ = conn.Close()
	}()
	defer func() {
		close(done)
		<-watcherDone
	}()

	guarded := s.read(conn)
	switch outcome(reason.Load()) {
	case stopped:
		s.liveAtEnd = guarded.ok && s.bookChecksAtEnd()
		return stopped, 0
	case dropped:
		s.stormed = true
		return dropped, 0
	}
	return closedByServer, guarded.wait
}

type readResult struct {
	ok   bool
	wait time.Duration
}

// read runs the protocol on conn until it fails, and reports whether the
// session reached a guarded live stream and, for a server close, the wait
// before the next dial.
func (s *socket) read(conn *websocket.Conn) readResult {
	r := s.run
	if err := conn.SetReadDeadline(time.Now().Add(welcomeTimeout)); err != nil {
		return readResult{wait: jitter(closeJitter)}
	}
	var f frame
	if err := conn.ReadJSON(&f); err != nil || f.Event != "welcome" || f.SnapshotID == "" {
		return s.closed(err)
	}
	if !s.welcomed {
		s.welcomed = true
		r.welcomed.Add(1)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return readResult{wait: jitter(closeJitter)}
	}
	for _, topic := range s.topics {
		if err := conn.WriteJSON(ws.ClientFrame{Event: ws.EventSubscribe, Topic: topic}); err != nil {
			return s.closed(err)
		}
	}
	guard := sequenceGuard()
	clear(s.books)
	pending := map[string]bool{}
	for _, topic := range s.topics {
		pending[topic] = true
	}
	ok := false
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			res := s.closed(err)
			res.ok = ok
			return res
		}
		recv := time.Now()
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil {
			r.errorFrames.Add(1)
			continue
		}
		if f.Event != "" {
			if f.Event == "error" {
				r.errorFrames.Add(1)
			}
			continue
		}
		r.frames.Add(1)
		r.observe(recv.Sub(time.UnixMilli(f.TS)))
		if !guard(f.DataFrame) {
			// The application drops its state on a gap and resyncs; the
			// resubscribe sends a fresh snapshot. Frames refused while the
			// resync is in flight count without a second resubscribe.
			r.gaps.Add(1)
			if s.gaps[f.Topic] {
				continue
			}
			s.gaps[f.Topic] = true
			for _, ev := range []ws.Event{ws.EventUnsubscribe, ws.EventSubscribe} {
				if err := conn.WriteJSON(ws.ClientFrame{Event: ev, Topic: f.Topic}); err != nil {
					res := s.closed(err)
					res.ok = ok
					return res
				}
			}
			continue
		}
		if f.Snapshot {
			r.snapshots.Add(1)
			if s.gaps[f.Topic] {
				delete(s.gaps, f.Topic)
				r.resynced.Add(1)
			}
		}
		if strings.HasSuffix(f.Topic, bookKind) {
			s.book(f.DataFrame, recv)
		}
		if f.Snapshot && pending[f.Topic] {
			delete(pending, f.Topic)
			if len(pending) == 0 {
				ok = true
				s.guardedAt(recv)
			}
		}
	}
}

// guardedAt records the first guarded live stream of the socket and, after
// the storm dropped it, its recovery.
func (s *socket) guardedAt(at time.Time) {
	r := s.run
	if !s.guardedOnce {
		s.guardedOnce = true
		if at.Before(r.report.Started.Add(r.profile.Recovery)) {
			r.guarded.Add(1)
		}
	}
	if !s.stormed {
		return
	}
	s.stormed = false
	took := at.Sub(time.Unix(0, r.stormAt.Load()))
	if took > r.profile.Recovery {
		return
	}
	r.recovered.Add(1)
	for {
		cur := r.maxRecovery.Load()
		if int64(took) <= cur || r.maxRecovery.CompareAndSwap(cur, int64(took)) {
			return
		}
	}
}

// closed classifies the end of a connection: a close frame of the server is
// counted, a 1013 must carry retry-after=<5..30>, and the client waits as the
// application client does before it redials.
func (s *socket) closed(err error) readResult {
	r := s.run
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		return readResult{wait: jitter(closeJitter)}
	}
	r.serverCloses.Add(1)
	m := retryAfter.FindStringSubmatch(ce.Text)
	var secs int
	if m != nil {
		secs, _ = strconv.Atoi(m[1])
	}
	hinted := m != nil && secs >= retryAfterMin && secs <= retryAfterMax
	if ce.Code == websocket.CloseTryAgainLater {
		r.closes1013.Add(1)
		if !hinted {
			r.noRetryAfter.Add(1)
		}
	}
	switch {
	case hinted:
		return readResult{wait: min(closeRetryMax, time.Duration(secs)*time.Second)}
	case ce.Code == websocket.CloseTryAgainLater:
		return readResult{wait: closeRetryDefault}
	}
	return readResult{wait: jitter(closeJitter)}
}

// book applies one frame of a book topic to the client book and checks the
// live frame bounds: at most BookFPS live frames in any second, at most
// BookLevels per side, a checksum equal to the CRC32 of the client book, and
// at most ChecksumEvery plus MaxDelay between two checksums.
func (s *socket) book(f ws.DataFrame, recv time.Time) {
	r, p := s.run, s.run.profile
	var d bookData
	if err := json.Unmarshal(f.Data, &d); err != nil {
		r.errorFrames.Add(1)
		return
	}
	b := s.books[f.Topic]
	if f.Snapshot || b == nil {
		s.books[f.Topic] = &bookTrack{asks: d.Asks, bids: d.Bids, lastChecksum: f.TS, lastRecv: recv}
		if !f.Snapshot {
			s.books[f.Topic].recent = []int64{f.TS}
		}
		return
	}
	r.bookFrames.Add(1)
	if len(d.Asks) > p.BookLevels || len(d.Bids) > p.BookLevels {
		r.bookLevels.Add(1)
	}
	window := f.TS - time.Second.Milliseconds()
	kept := b.recent[:0]
	for _, ts := range b.recent {
		if ts > window {
			kept = append(kept, ts)
		}
	}
	b.recent = append(kept, f.TS)
	if len(b.recent) > p.BookFPS {
		r.bookRate.Add(1)
	}
	b.asks, b.bids = d.Asks, d.Bids
	if d.Checksum == nil {
		return
	}
	r.checksums.Add(1)
	if Checksum(b.asks, b.bids, p.BookLevels) != *d.Checksum {
		r.mismatches.Add(1)
	}
	if time.Duration(f.TS-b.lastChecksum)*time.Millisecond > p.ChecksumEvery+p.MaxDelay {
		r.late.Add(1)
	}
	b.lastChecksum, b.lastRecv = f.TS, recv
}

// bookChecksAtEnd checks the interval from the last checksum of every book
// topic to the end of the run and reports whether every topic held a book.
func (s *socket) bookChecksAtEnd() bool {
	r, p := s.run, s.run.profile
	end := time.Unix(0, r.endAt.Load())
	held := 0
	for _, topic := range s.topics {
		b, ok := s.books[topic]
		if !ok {
			continue
		}
		held++
		if end.Sub(b.lastRecv) > p.ChecksumEvery+p.MaxDelay {
			r.late.Add(1)
		}
	}
	want := 0
	for _, topic := range s.topics {
		if strings.HasSuffix(topic, bookKind) {
			want++
		}
	}
	return held == want
}

// Checksum is the CRC32 (IEEE) the client computes over the top depth levels
// of its book written as price:size pairs joined by commas, asks then bids
// (contracts/data-services.md section 10).
func Checksum(asks, bids []feeds.Level, depth int) uint32 {
	pairs := make([]string, 0, 2*depth)
	for _, side := range [][]feeds.Level{asks[:min(len(asks), depth)], bids[:min(len(bids), depth)]} {
		for _, l := range side {
			pairs = append(pairs, strconv.FormatFloat(l[0], 'f', -1, 64)+":"+strconv.FormatFloat(l[1], 'f', -1, 64))
		}
	}
	return crc32.ChecksumIEEE([]byte(strings.Join(pairs, ",")))
}

// sequenceGuard is the per-topic sequence rule of the application client
// (Hyperpax:src/core/net/realtime.ts:29-47): it refuses a live frame of
// another snapshotId or beyond the next seq, and a snapshot never rolls the
// baseline of the same snapshotId backwards.
func sequenceGuard() func(ws.DataFrame) bool {
	type mark struct {
		seq   uint64
		epoch string
	}
	topics := map[string]mark{}
	return func(f ws.DataFrame) bool {
		old, ok := topics[f.Topic]
		if f.Snapshot || !ok {
			if !ok || old.epoch != f.SnapshotID || f.Seq >= old.seq {
				topics[f.Topic] = mark{f.Seq, f.SnapshotID}
			}
			return true
		}
		if old.epoch != f.SnapshotID || f.Seq > old.seq+1 {
			return false
		}
		if f.Seq <= old.seq {
			return true
		}
		topics[f.Topic] = mark{f.Seq, f.SnapshotID}
		return true
	}
}

func jitter(ceiling time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(ceiling)))
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
