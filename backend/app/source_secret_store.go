package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
)

var ErrSourceSecretNotFound = errors.New("source secret not found")
var sourceSecretKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type SourceSecretStore interface {
	Load(context.Context, string) ([]byte, error)
	Save(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

type memorySourceSecretStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func NewMemorySourceSecretStore() SourceSecretStore {
	return &memorySourceSecretStore{values: make(map[string][]byte)}
}
func validateSourceSecretKey(key string) error {
	if !sourceSecretKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid source secret key")
	}
	return nil
}
func (s *memorySourceSecretStore) Load(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSourceSecretKey(key); err != nil {
		return nil, err
	}
	value, ok := s.values[key]
	if !ok {
		return nil, ErrSourceSecretNotFound
	}
	return append([]byte(nil), value...), nil
}
func (s *memorySourceSecretStore) Save(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSourceSecretKey(key); err != nil {
		return err
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}
func (s *memorySourceSecretStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSourceSecretKey(key); err != nil {
		return err
	}
	delete(s.values, key)
	return nil
}
