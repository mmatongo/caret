// Package keychain provides secure storage for API keys
package keychain

import (
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

const appName = "caret"

type Service struct {
	mu    sync.RWMutex
	cache map[string]string
}

func New() *Service {
	return &Service{cache: make(map[string]string)}
}

func (s *Service) Get(provider string) (string, error) {
	s.mu.RLock()
	if v, ok := s.cache[provider]; ok {
		s.mu.RUnlock()
		return v, nil
	}
	s.mu.RUnlock()

	v, err := keyring.Get(appName, provider)
	if err != nil {
		return "", fmt.Errorf("keychain: get %q: %w", provider, err)
	}

	s.mu.Lock()
	s.cache[provider] = v
	s.mu.Unlock()

	return v, nil
}

func (s *Service) Set(provider, key string) error {
	if err := keyring.Set(appName, provider, key); err != nil {
		return fmt.Errorf("keychain: set %q: %w", provider, err)
	}
	s.mu.Lock()
	s.cache[provider] = key
	s.mu.Unlock()
	return nil
}

func (s *Service) Delete(provider string) error {
	if err := keyring.Delete(appName, provider); err != nil {
		return fmt.Errorf("keychain: delete %q: %w", provider, err)
	}
	s.mu.Lock()
	delete(s.cache, provider)
	s.mu.Unlock()
	return nil
}

// Has returns true when a key exists for the given provider
func (s *Service) Has(provider string) bool {
	_, err := s.Get(provider)
	// TODO: handle errors better
	return err == nil
}
