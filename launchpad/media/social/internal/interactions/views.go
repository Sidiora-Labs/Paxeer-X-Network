package interactions

import "context"

// CommentsPage returns one visible page of a pool's comments (newest first)
// and the next cursor, with the same projection the /v2/social comment list
// serves. Callers mounting their own route surface use it instead of the
// legacy-shaped handler.
func (h *HTTP) CommentsPage(ctx context.Context, pool string, limit int, cursor string) ([]CommentView, *string, error) {
	if h == nil || h.Store.DB == nil {
		return nil, nil, ErrDependency
	}
	return h.comments(ctx, pool, limit, cursor)
}

// CommentByID returns the visible projection of one comment; a removed or
// erased comment is pgx.ErrNoRows.
func (h *HTTP) CommentByID(ctx context.Context, id string) (CommentView, error) {
	if h == nil || h.Store.DB == nil {
		return CommentView{}, ErrDependency
	}
	return h.comment(ctx, id)
}
