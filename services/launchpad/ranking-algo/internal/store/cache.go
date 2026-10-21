package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/redis/go-redis/v9"
)

type SnapshotCache struct {
	redis  *redis.Client
	prefix string
}

func NewSnapshotCache(client *redis.Client, namespace string) (*SnapshotCache, error) {
	if client == nil || !regexp.MustCompile(`^[a-z][a-z0-9_-]{2,62}$`).MatchString(namespace) {
		return nil, ranker.ErrUnavailable
	}
	return &SnapshotCache{client, "ranking:" + namespace + ":"}, nil
}
func (c *SnapshotCache) key(h ranker.Header, offset int64, limit int) string {
	return c.prefix + h.Category + ":" + h.RunID + ":" + h.OutputDigest + ":" + strconv.FormatInt(offset, 10) + ":" + strconv.Itoa(limit)
}
func (c *SnapshotCache) Get(ctx context.Context, h ranker.Header, offset int64, limit int) (ranker.StoredPage, error) {
	var page ranker.StoredPage
	if h.Validate() != nil || offset < 0 || offset > h.Total || limit < 1 || limit > 100 {
		return page, ranker.ErrUnavailable
	}
	raw, err := c.redis.Get(ctx, c.key(h, offset, limit)).Bytes()
	if err != nil {
		return page, err
	}
	if len(raw) > 65536 {
		return page, ranker.ErrUnavailable
	}
	if err = json.Unmarshal(raw, &page); err != nil {
		return page, err
	}
	actual, expected := page.Header, h
	actual.CompletedAt = time.Time{}
	expected.CompletedAt = time.Time{}
	count := h.Total - offset
	if count > int64(limit) {
		count = int64(limit)
	}
	if actual != expected || !page.Header.CompletedAt.Equal(h.CompletedAt) || page.Offset != offset || int64(len(page.Items)) != count {
		return page, ranker.ErrUnavailable
	}
	return page, page.Validate()
}
func (c *SnapshotCache) Put(ctx context.Context, page ranker.StoredPage, limit int) error {
	if page.Validate() != nil || limit < 1 || limit > 100 || len(page.Items) > limit {
		return ranker.ErrUnavailable
	}
	count := page.Header.Total - page.Offset
	if count > int64(limit) {
		count = int64(limit)
	}
	if int64(len(page.Items)) != count {
		return ranker.ErrUnavailable
	}
	raw, err := json.Marshal(page)
	if err != nil {
		return err
	}
	if len(raw) > 65536 {
		return ranker.ErrUnavailable
	}
	return c.redis.Set(ctx, c.key(page.Header, page.Offset, limit), raw, 15*time.Minute).Err()
}
func (c *SnapshotCache) Rebuild(ctx context.Context, s *SnapshotStore, category string) error {
	h, err := s.Current(ctx, category)
	if err != nil {
		return err
	}
	page, err := s.Page(ctx, h, 0, 100)
	if err != nil {
		return err
	}
	if err = c.Put(ctx, page, 100); err != nil {
		return err
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	script := redis.NewScript(`local old=redis.call('GET',KEYS[2]); if old and (#old>#ARGV[2] or (#old==#ARGV[2] and old>ARGV[2])) then return 0 end; redis.call('SET',KEYS[1],ARGV[1],'EX',900);redis.call('SET',KEYS[2],ARGV[2],'EX',900);return 1`)
	_, err = script.Run(ctx, c.redis, []string{c.prefix + "current:" + category, c.prefix + "current:" + category + ":revision"}, raw, fmt.Sprint(h.Revision)).Result()
	return err
}
