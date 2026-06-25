package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisChallengeStore struct {
	client *redis.Client
	prefix string
}

func NewRedisChallengeStore(client *redis.Client, prefix string) *RedisChallengeStore {
	if prefix == "" {
		prefix = "meme:siwe:"
	}
	return &RedisChallengeStore{client: client, prefix: prefix}
}

func (s *RedisChallengeStore) Save(ctx context.Context, address string, challenge SIWEChallenge, ttl time.Duration) error {
	data, err := json.Marshal(challenge)
	if err != nil {
		return fmt.Errorf("marshal challenge: %w", err)
	}
	if ttl <= 0 {
		ttl = time.Until(challenge.ExpirationTime)
	}
	return s.client.Set(ctx, s.key(address), data, ttl).Err()
}

func (s *RedisChallengeStore) Get(ctx context.Context, address string) (SIWEChallenge, error) {
	data, err := s.client.Get(ctx, s.key(address)).Bytes()
	if err == redis.Nil {
		return SIWEChallenge{}, ErrInvalidNonce
	}
	if err != nil {
		return SIWEChallenge{}, err
	}
	var challenge SIWEChallenge
	if err := json.Unmarshal(data, &challenge); err != nil {
		return SIWEChallenge{}, fmt.Errorf("unmarshal challenge: %w", err)
	}
	return challenge, nil
}

func (s *RedisChallengeStore) Delete(ctx context.Context, address string) error {
	deleted, err := s.client.Del(ctx, s.key(address)).Result()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrInvalidNonce
	}
	return nil
}

func (s *RedisChallengeStore) key(address string) string {
	return s.prefix + address
}
