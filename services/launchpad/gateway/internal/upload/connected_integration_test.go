package upload

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestConnectedAdmissionCapsAcrossInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	namespace := "l41-connected-" + time.Now().Format("150405000000000")
	first, e := NewAdmission(db, namespace, 2)
	if e != nil {
		t.Fatal(e)
	}
	second, e := NewAdmission(db, namespace, 2)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		keys, e := db.Keys(context.Background(), namespace+":*").Result()
		if e == nil && len(keys) > 0 {
			db.Del(context.Background(), keys...)
		}
	})
	wallet := "0x" + strings.Repeat("a", 40)
	var mu sync.Mutex
	var releases []func()
	var acquired int
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			admission := first
			if index%2 == 1 {
				admission = second
			}
			release, e := admission.Acquire(ctx, wallet)
			if e == nil {
				mu.Lock()
				acquired++
				releases = append(releases, release)
				mu.Unlock()
			}
		}(index)
	}
	group.Wait()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	if acquired != 2 {
		t.Fatal("distributed wallet/global admission", acquired)
	}
}
