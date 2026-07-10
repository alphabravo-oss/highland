package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisBackend stores sessions for multi-replica HA API.
type RedisBackend struct {
	rdb    *redis.Client
	prefix string
}

// NewRedisBackend connects to Redis. addr e.g. highland-redis:6379
func NewRedisBackend(addr, password string, db int) (*RedisBackend, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &RedisBackend{rdb: rdb, prefix: "highland:sess:"}, nil
}

func (r *RedisBackend) key(id string) string { return r.prefix + id }

func (r *RedisBackend) Create(user User, ttl time.Duration) (string, error) {
	id, err := randomID(32)
	if err != nil {
		return "", err
	}
	sess := Session{ID: id, User: user, ExpiresAt: time.Now().Add(ttl)}
	b, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	if err := r.rdb.Set(ctx, r.key(id), b, ttl).Err(); err != nil {
		return "", err
	}
	return id, nil
}

func (r *RedisBackend) Get(id string) (*Session, bool) {
	ctx := context.Background()
	b, err := r.rdb.Get(ctx, r.key(id)).Bytes()
	if err != nil {
		return nil, false
	}
	var sess Session
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		r.Delete(id)
		return nil, false
	}
	return &sess, true
}

func (r *RedisBackend) Delete(id string) {
	_ = r.rdb.Del(context.Background(), r.key(id)).Err()
}

// redisStateStore persists OIDC CSRF state in Redis so the auth-code flow works
// across API replicas. It reuses the session Redis client.
type redisStateStore struct {
	rdb    *redis.Client
	prefix string
}

func newRedisStateStore(rdb *redis.Client) *redisStateStore {
	return &redisStateStore{rdb: rdb, prefix: "highland:oidcstate:"}
}

func (r *redisStateStore) put(state string, ttl time.Duration) {
	_ = r.rdb.Set(context.Background(), r.prefix+state, "1", ttl).Err()
}

// consume atomically deletes the state key, returning true only if it existed
// (and had not yet expired). Expired keys are removed by Redis TTL.
func (r *redisStateStore) consume(state string) bool {
	n, err := r.rdb.Del(context.Background(), r.prefix+state).Result()
	return err == nil && n > 0
}
