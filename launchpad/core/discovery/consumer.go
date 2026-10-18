package discovery

import (
	"context"
	"time"
)

type PublicResource struct {
	Ref                SourceRef
	Body, Language     string
	Topics             []string
	PositionDisclosure string
	PositionVerified   bool
}
type SourceAuthority interface {
	CanonicalPool(context.Context, string) (bool, error)
	ResolvePublic(context.Context, string, string) (PublicResource, error)
	VerifiedAccount(context.Context, string) (bool, error)
}
type PublicEvent struct {
	Consumer, EventID string
	Source            SourceRef
	ObservedAt        time.Time
}

func (s Store) Consume(ctx context.Context, a SourceAuthority, event PublicEvent) error {
	if a == nil {
		return ErrUnavailable
	}
	if event.Source.Validate() != nil {
		return ErrInvalid
	}
	if !event.Source.Tombstone {
		resource, e := a.ResolvePublic(ctx, event.Source.Kind, event.Source.ID)
		if e != nil {
			return e
		}
		if !sameSource(resource.Ref, event.Source) || !resource.Ref.Public || !resource.Ref.Authorized {
			return ErrForbidden
		}
	}
	for _, pool := range event.Source.Pools {
		ok, e := a.CanonicalPool(ctx, pool)
		if e != nil {
			return e
		}
		if !ok {
			return ErrInvalid
		}
	}
	return s.Apply(ctx, event.Consumer, event.EventID, event.Source, event.ObservedAt)
}

type PublicationPolicy interface {
	ReviewCallout(context.Context, string, string, string) (ModerationDecision, error)
}
