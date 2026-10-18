package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"time"
)

type Feed struct {
	Store       Store
	Sources     SourceAuthority
	Preferences PreferencesConsumer
	Graph       GraphConsumer
	Signer      CursorSigner
}
type FeedCard struct {
	Source                                                  SourceRef
	Body, State, PositionDisclosure, PositionDisclosureKind string
	PositionVerified                                        bool
}
type FeedPage struct {
	Entries                                     []FeedCard
	Cursor, Algorithm, Snapshot, FallbackReason string
	Personalized                                bool
}
type feedSnapshot struct{ Refs []SourceRef }

func (f Feed) Page(ctx context.Context, account, order, token string, limit int, now time.Time) (FeedPage, error) {
	page := FeedPage{Entries: []FeedCard{}}
	if f.Sources == nil || f.Graph == nil || limit < 1 || limit > 50 {
		return page, ErrInvalid
	}
	if order != "chronological" && order != "relevance" {
		return page, ErrInvalid
	}
	page.Algorithm = "chronological-v1"
	if order == "relevance" {
		page.Algorithm = "language-follow-topic-v1"
	}
	var pref PreferenceSnapshot
	var graph GraphSnapshot
	if account != "" {
		pref, graph, page.Personalized, page.FallbackReason = f.projections(ctx, account, now)
		if !page.Personalized {
			pref = PreferenceSnapshot{}
			graph = GraphSnapshot{}
			page.Algorithm = "public-chronological-fallback-v1"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c := Cursor{AccountID: account, Algorithm: page.Algorithm, PreferenceRevision: pref.Revision, GraphRevision: graph.Revision, ExpiresAt: now.Add(60 * time.Second)}
	var snap feedSnapshot
	if token != "" {
		var e error
		c, e = f.Signer.Verify(token, account, pref.Revision, graph.Revision, now)
		if e != nil {
			return page, e
		}
		if c.Algorithm != page.Algorithm {
			return page, ErrConflict
		}
		var b []byte
		e = f.Store.DB.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.snapshots WHERE id=$1 AND account_id=$2 AND expires_at>$3`, c.Snapshot, account, now).Scan(&b)
		if e != nil {
			return page, e
		}
		if e = json.Unmarshal(b, &snap); e != nil {
			return page, e
		}
	} else {
		refs, e := f.Store.Sources(ctx, now)
		if e != nil {
			return page, e
		}
		all := []SourceRef{}
		for offset := 0; offset < len(refs); offset += 50 {
			batch, err := Candidates(refs[offset:min(offset+50, len(refs))], now, now, 50)
			if err != nil {
				return page, err
			}
			all = append(all, batch...)
		}
		refs = all
		scores := map[string]int{}
		for _, ref := range refs {
			if page.Personalized && (slices.Contains(graph.BlockedActorIDs, ref.CreatorPublicID) || slices.Contains(pref.MutedActorIDs, ref.CreatorPublicID) || slices.Contains(pref.MutedResourceIDs, ref.ID)) {
				continue
			}
			resource, e := f.resolve(ctx, account, ref, now)
			if e != nil || resource.State != "available" {
				continue
			}
			snap.Refs = append(snap.Refs, ref)
			if order == "relevance" && page.Personalized {
				public, e := f.Sources.ResolvePublic(ctx, ref.Kind, ref.ID)
				if e != nil {
					continue
				}
				if slices.Contains(pref.Languages, public.Language) {
					scores[ref.ID] += 4
				}
				if slices.Contains(graph.FollowedActorIDs, ref.CreatorPublicID) {
					scores[ref.ID] += 2
				}
				for _, topic := range public.Topics {
					if slices.Contains(pref.FollowedTopics, topic) {
						scores[ref.ID]++
					}
				}
			}
		}
		if order == "relevance" && page.Personalized {
			sort.SliceStable(snap.Refs, func(i, j int) bool { return scores[snap.Refs[i].ID] > scores[snap.Refs[j].ID] })
		}
		var id [24]byte
		if _, e = rand.Read(id[:]); e != nil {
			return page, e
		}
		c.Snapshot = hex.EncodeToString(id[:])
		_, e = f.Store.DB.ExecContext(ctx, `INSERT INTO narrative_discovery.snapshots(id,account_id,preference_revision,graph_revision,algorithm,document,expires_at)VALUES($1,$2,$3,$4,$5,$6,$7)`, c.Snapshot, account, pref.Revision, graph.Revision, page.Algorithm, encode(snap), c.ExpiresAt)
		if e != nil {
			return page, e
		}
	}
	end := min(c.Offset+limit, len(snap.Refs))
	if c.Offset > len(snap.Refs) {
		return page, ErrInvalid
	}
	for _, ref := range snap.Refs[c.Offset:end] {
		card, e := f.resolve(ctx, account, ref, now)
		if e != nil {
			card = FeedCard{Source: ref, State: "unavailable"}
		}
		page.Entries = append(page.Entries, card)
	}
	page.Snapshot = c.Snapshot
	if end < len(snap.Refs) {
		c.Offset = end
		var e error
		page.Cursor, e = f.Signer.Sign(c)
		if e != nil {
			return page, e
		}
	}
	return page, nil
}
func (f Feed) resolve(ctx context.Context, account string, ref SourceRef, now time.Time) (FeedCard, error) {
	card := FeedCard{Source: ref, State: "unavailable"}
	current, e := f.Store.Source(ctx, ref.ID)
	if e != nil {
		return card, e
	}
	card.Source = current
	if current.Tombstone {
		card.State = "removed"
		return card, nil
	}
	if !current.Public || !current.Authorized {
		return card, ErrForbidden
	}
	decision, e := f.Graph.ResolveVisibility(ctx, current.ID, account)
	if e != nil || !decision.Visible || decision.ResourceID != current.ID || decision.Revision < current.VisibilityRevision || decision.ObservedAt.After(now) || now.Sub(decision.ObservedAt) > 60*time.Second {
		return card, ErrForbidden
	}
	resource, e := f.Sources.ResolvePublic(ctx, current.Kind, current.ID)
	if e != nil {
		return card, e
	}
	if !sameSource(resource.Ref, current) || !resource.Ref.Public || !resource.Ref.Authorized || resource.Ref.Tombstone {
		return card, ErrUnavailable
	}
	card.Body = resource.Body
	card.State = "available"
	card.PositionDisclosure = resource.PositionDisclosure
	if resource.PositionDisclosure != "" {
		card.PositionDisclosureKind = "user_declared"
	}
	card.PositionVerified = resource.PositionVerified
	return card, nil
}
func (f Feed) Detail(ctx context.Context, account, id string, now time.Time) (FeedCard, error) {
	ref, e := f.Store.Source(ctx, id)
	if e != nil {
		return FeedCard{}, e
	}
	return f.resolve(ctx, account, ref, now)
}

func (f Feed) projections(ctx context.Context, account string, now time.Time) (PreferenceSnapshot, GraphSnapshot, bool, string) {
	var pref PreferenceSnapshot
	var graph GraphSnapshot
	if f.Preferences == nil {
		return pref, graph, false, "preference_unavailable"
	}
	read, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var e error
	pref, e = f.Preferences.Preferences(read, account)
	if e != nil || pref.AccountID != account || !pref.Linked || pref.Revision == 0 || pref.ObservedAt.After(now) || now.Sub(pref.ObservedAt) > 60*time.Second {
		return PreferenceSnapshot{}, GraphSnapshot{}, false, "preference_unavailable"
	}
	graph, e = f.Graph.Graph(read, account)
	if e != nil || graph.AccountID != account || graph.Revision == 0 || graph.ObservedAt.After(now) || now.Sub(graph.ObservedAt) > 60*time.Second {
		return PreferenceSnapshot{}, GraphSnapshot{}, false, "graph_unavailable"
	}
	return pref, graph, true, ""
}
