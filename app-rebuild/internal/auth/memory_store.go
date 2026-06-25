package auth

import (
	"context"
	"sync"
	"time"
)

type MemoryChallengeStore struct {
	mu                  sync.Mutex
	challengesByAddress map[string]SIWEChallenge
}

func NewMemoryChallengeStore() *MemoryChallengeStore {
	return &MemoryChallengeStore{challengesByAddress: make(map[string]SIWEChallenge)}
}

func (s *MemoryChallengeStore) Save(_ context.Context, address string, challenge SIWEChallenge, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challengesByAddress[address] = challenge
	return nil
}

func (s *MemoryChallengeStore) Get(_ context.Context, address string) (SIWEChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challengesByAddress[address]
	if !ok || time.Now().After(challenge.ExpirationTime) {
		return SIWEChallenge{}, ErrInvalidNonce
	}
	return challenge, nil
}

func (s *MemoryChallengeStore) Delete(_ context.Context, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challengesByAddress[address]
	if !ok || time.Now().After(challenge.ExpirationTime) {
		return ErrInvalidNonce
	}
	delete(s.challengesByAddress, address)
	return nil
}
