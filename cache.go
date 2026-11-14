package main

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"goPinger/query"
)

var redisClient *redis.Client

func initRedis(ctx context.Context, url string) error {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return err
	}
	redisClient = redis.NewClient(opts)
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return redisClient.Ping(c).Err()
}

func keyType(a string) string  { return "gp:type:" + a }
func keyState(a string) string { return "gp:state:" + a }
func keyLock(a string) string  { return "gp:lock:" + a }
func keyNext(id int64) string  { return "gp:next:" + strconv.FormatInt(id, 10) }
func keyUser(t string) string  { return "gp:user:" + t }
func keyList(u string) string  { return "gp:list:" + u }

func getCachedType(ctx context.Context, a string) (query.ServerType, bool) {
	n, err := redisClient.Get(ctx, keyType(a)).Int()
	if err != nil {
		return query.ServerAuto, false
	}
	return query.ServerType(n), true
}

func cacheType(ctx context.Context, a string, t query.ServerType) {
	redisClient.Set(ctx, keyType(a), int(t), 0)
}

func isDue(ctx context.Context, id int64) bool {
	n, err := redisClient.Exists(ctx, keyNext(id)).Result()
	return err == nil && n == 0
}

func acquirePoll(ctx context.Context, id int64) bool {
	ok, err := redisClient.SetNX(ctx, keyNext(id), 1, cfg.PollLockTTL).Result()
	return err == nil && ok
}

func markPolled(ctx context.Context, id int64, d time.Duration) {
	redisClient.Set(ctx, keyNext(id), 1, d)
}

// remainingPoll — сколько осталось до следующего опроса сервера id.
// -1 если опрос не запланирован (disabled или только что создан).
func remainingPoll(ctx context.Context, id int64) time.Duration {
	ttl, err := redisClient.TTL(ctx, keyNext(id)).Result()
	if err != nil || ttl <= 0 {
		return -1
	}
	return ttl
}

func clearUserCache(ctx context.Context, token string) {
	redisClient.Del(ctx, keyUser(token))
}

func cachedUserToken(ctx context.Context, token string) (User, error) {
	if b, err := redisClient.Get(ctx, keyUser(token)).Bytes(); err == nil {
		var u User
		if json.Unmarshal(b, &u) == nil {
			return u, nil
		}
	}
	u, err := readUserByToken(ctx, token)
	if err != nil {
		return u, err
	}
	if b, err := json.Marshal(u); err == nil {
		redisClient.Set(ctx, keyUser(token), b, cfg.UserCacheTTL)
	}
	return u, nil
}

// acquireServerLock — глобальный per-address lock. Проигравший не идёт в сеть.
func acquireServerLock(ctx context.Context, a string) (func(), bool) {
	ttl := cfg.QueryTimeout + 5*time.Second
	ok, err := redisClient.SetNX(ctx, keyLock(a), "1", ttl).Result()
	if err != nil || !ok {
		return nil, false
	}
	return func() { redisClient.Del(context.Background(), keyLock(a)) }, true
}

// waitForCache ждёт появления кеша, пока lock держится. Ранний выход,
// если writer снял lock без сохранения (упал).
func waitForCache(ctx context.Context, a string) (*ServerState, bool) {
	deadline := time.Now().Add(cfg.LockWaitTimeout)
	for time.Now().Before(deadline) {
		if st, _, ok := getStateCache(ctx, a); ok {
			return st, true
		}
		if n, _ := redisClient.Exists(ctx, keyLock(a)).Result(); n == 0 {
			time.Sleep(50 * time.Millisecond)
			if st, _, ok := getStateCache(ctx, a); ok {
				return st, true
			}
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, false
}

func getStateCache(ctx context.Context, a string) (*ServerState, time.Duration, bool) {
	b, err := redisClient.Get(ctx, keyState(a)).Bytes()
	if err != nil {
		return nil, 0, false
	}
	var st ServerState
	if json.Unmarshal(b, &st) != nil {
		return nil, 0, false
	}
	ttl, err := redisClient.TTL(ctx, keyState(a)).Result()
	if err != nil || ttl <= 0 {
		return nil, 0, false
	}
	return &st, ttl, true
}

func saveStateCache(ctx context.Context, a string, st *ServerState, ttl time.Duration) {
	if b, err := json.Marshal(st); err == nil {
		redisClient.Set(ctx, keyState(a), b, ttl)
	}
}

func invalidateListCache(ctx context.Context, userID string) {
	redisClient.Del(ctx, keyList(userID))
}
