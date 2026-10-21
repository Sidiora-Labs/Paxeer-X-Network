package discovery

import (
	"context"
)

type OwnedSources struct {
	External SourceAuthority
	Callouts CalloutService
}

func (o OwnedSources) CanonicalPool(ctx context.Context, pool string) (bool, error) {
	if o.External == nil {
		return false, ErrUnavailable
	}
	return o.External.CanonicalPool(ctx, pool)
}
func (o OwnedSources) VerifiedAccount(ctx context.Context, actor string) (bool, error) {
	if o.External == nil {
		return false, ErrUnavailable
	}
	return o.External.VerifiedAccount(ctx, actor)
}
func (o OwnedSources) ResolvePublic(ctx context.Context, kind, id string) (PublicResource, error) {
	if kind != "callout" {
		if o.External == nil {
			return PublicResource{}, ErrUnavailable
		}
		return o.External.ResolvePublic(ctx, kind, id)
	}
	c, e := o.Callouts.Read(ctx, id)
	if e != nil {
		return PublicResource{}, e
	}
	ref, e := o.Callouts.Store.Source(ctx, id)
	if e != nil {
		return PublicResource{}, e
	}
	if c.Deleted || c.ModerationStatus != "approved" || ref.Tombstone {
		return PublicResource{}, ErrUnavailable
	}
	if c.Revision != ref.SourceRevision || c.AuthorAccountID != ref.CreatorPublicID || !c.CreatedAt.Equal(ref.PublishedAt) || ref.ID != c.ID || ref.Kind != "callout" || len(ref.Pools) != 1 || ref.Pools[0] != c.PoolAddress {
		return PublicResource{}, ErrUnavailable
	}
	return PublicResource{Ref: ref, Body: c.Body, PositionDisclosure: c.PositionDisclosure, PositionVerified: false}, nil
}

func (o OwnedSources) ReviewCallout(ctx context.Context, actor, pool, body string) (ModerationDecision, error) {
	policy, ok := o.External.(PublicationPolicy)
	if !ok {
		return ModerationDecision{}, ErrUnavailable
	}
	return policy.ReviewCallout(ctx, actor, pool, body)
}
