package streams

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/db/sqlcdb"
)

const (
	viewerEntryTTL = 60 * time.Second   // a heartbeat keeps a viewer "active" 60s
	viewerSetTTL   = 3600 * time.Second // the viewer set self-expires after 1h
	vcThrottleTTL  = 10 * time.Second   // DB viewer_count writes throttled to 10s
)

// countViewers records a viewer heartbeat and returns the current active viewer
// count for the stream, pruning stale members. It mirrors the TS heartbeat
// logic: a per-viewer key expires after 60s, and the set is reconciled against
// those keys on each beat. The DB viewer_count is refreshed at most once per 10s.
func (h *Handlers) countViewers(ctx context.Context, streamID, viewerID string) (int, error) {
	setKey := "stream:viewers:" + streamID
	entryKey := viewerEntryKey(streamID, viewerID)

	if err := h.redis.Set(ctx, entryKey, "1", viewerEntryTTL).Err(); err != nil {
		return 0, err
	}
	if err := h.redis.SAdd(ctx, setKey, viewerID).Err(); err != nil {
		return 0, err
	}
	if err := h.redis.Expire(ctx, setKey, viewerSetTTL).Err(); err != nil {
		return 0, err
	}

	members, err := h.redis.SMembers(ctx, setKey).Result()
	if err != nil {
		return 0, err
	}

	active := 0
	if len(members) > 0 {
		pipe := h.redis.Pipeline()
		cmds := make([]*goredis.IntCmd, len(members))
		for i, m := range members {
			cmds[i] = pipe.Exists(ctx, viewerEntryKey(streamID, m))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return 0, err
		}
		var stale []string
		for i, c := range cmds {
			if c.Val() == 1 {
				active++
			} else {
				stale = append(stale, members[i])
			}
		}
		if len(stale) > 0 {
			if err := h.redis.SRem(ctx, setKey, stale).Err(); err != nil {
				return 0, err
			}
		}
	}

	h.maybePersistViewerCount(ctx, streamID, active)
	return active, nil
}

// maybePersistViewerCount writes viewer_count to the DB at most once per
// vcThrottleTTL (SET NX EX). Failures are logged, never fatal to the heartbeat.
func (h *Handlers) maybePersistViewerCount(ctx context.Context, streamID string, active int) {
	throttleKey := "stream:vc_throttle:" + streamID
	ok, err := h.redis.SetNX(ctx, throttleKey, "1", vcThrottleTTL).Result()
	if err != nil {
		h.logErr("viewer count throttle", err)
		return
	}
	if !ok {
		return
	}
	if err := h.q.UpdateViewerCount(ctx, sqlcdb.UpdateViewerCountParams{
		ID:          streamID,
		ViewerCount: int64(active),
	}); err != nil {
		h.logErr("update viewer count", err)
	}
}

func viewerEntryKey(streamID, viewerID string) string {
	return "stream:viewer:" + streamID + ":" + viewerID
}
