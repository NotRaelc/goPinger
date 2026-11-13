package main

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"goPinger/query"
)

func keyType(address string) string  { return "gp:type:" + address }
func keyState(address string) string { return "gp:state:" + address }
func keyNext(id int64) string        { return "gp:next:" + strconv.FormatInt(id, 10) }
func keyUser(token string) string    { return "gp:user:" + token }

func getCachedType(ctx context.Context, address string) (query.ServerType, bool) {
	n, err := redisClient.Get(ctx, keyType(address)).Int()
	if err != nil {
		return query.ServerAuto, false
	}
	return query.ServerType(n), true
}

func cacheType(ctx context.Context, address string, t query.ServerType) {
	redisClient.Set(ctx, keyType(address), int(t), 0)
}

func isDue(ctx context.Context, id int64) bool {
	n, err := redisClient.Exists(ctx, keyNext(id)).Result()
	return err == nil && n == 0
}

func acquirePoll(ctx context.Context, id int64) bool {
	ok, err := redisClient.SetNX(ctx, keyNext(id), 1, cfg.PollLockTTL).Result()
	return err == nil && ok
}

func markPolled(ctx context.Context, id int64, interval time.Duration) {
	redisClient.Set(ctx, keyNext(id), 1, interval)
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

// Кеш последнего успешного состояния, разделяемый между всеми пользователями,
// которые добавили один и тот же адрес. TTL ключа == интервалу опроса.
func getStateCache(ctx context.Context, address string) (*ServerState, time.Duration, bool) {
	b, err := redisClient.Get(ctx, keyState(address)).Bytes()
	if err != nil {
		return nil, 0, false
	}
	var st ServerState
	if json.Unmarshal(b, &st) != nil {
		return nil, 0, false
	}
	ttl, err := redisClient.TTL(ctx, keyState(address)).Result()
	if err != nil || ttl <= 0 {
		return nil, 0, false
	}
	return &st, ttl, true
}

func saveStateCache(ctx context.Context, address string, st *ServerState, ttl time.Duration) {
	if b, err := json.Marshal(st); err == nil {
		redisClient.Set(ctx, keyState(address), b, ttl)
	}
}

func extendStateCache(ctx context.Context, address string, ttl time.Duration) {
	redisClient.Expire(ctx, keyState(address), ttl)
}

func keyLastPoll(addr string) string { return "gp:lastpoll:" + addr }
func keyLock(addr string) string     { return "gp:lock:" + addr }

func getLastPoll(ctx context.Context, addr string) (time.Time, bool) {
	ms, err := redisClient.Get(ctx, keyLastPoll(addr)).Int64()
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}

func setLastPoll(ctx context.Context, addr string, t time.Time) {
	redisClient.Set(ctx, keyLastPoll(addr), t.UnixMilli(), 24*time.Hour)
}

// acquireServerLock берёт глобальный lock на адрес. Пока lock жив, ни один
// другой пользователь не может инициировать реальный запрос к этому адресу.
// При успехе сразу проставляет lastpoll = now, чтобы даже при падении
// запроса не было искушения долбить сервер снова.
func acquireServerLock(ctx context.Context, addr string) (func(), bool) {
	ttl := cfg.QueryTimeout + 5*time.Second
	ok, err := redisClient.SetNX(ctx, keyLock(addr), "1", ttl).Result()
	if err != nil || !ok {
		return nil, false
	}
	setLastPoll(ctx, addr, time.Now())
	return func() { redisClient.Del(context.Background(), keyLock(addr)) }, true
}

// waitForCache ждёт появления кеша (например, пока другой пользователь
// завершает реальный запрос) в течение cfg.LockWaitTimeout.
func waitForCache(ctx context.Context, addr string) (*ServerState, bool) {
	deadline := time.Now().Add(cfg.LockWaitTimeout)
	for time.Now().Before(deadline) {
		if st, _, ok := getStateCache(ctx, addr); ok {
			return st, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, false
}
