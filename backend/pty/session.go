package pty

import (
	"os"
	"sync"
)

type Session struct {
	id     string
	pty    *os.File
	cmd    interface{ Wait() error }
	mu     sync.Mutex
	closed bool
}

func (s *Session) ID() string { return s.id }

func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	_, err := s.pty.Write(data)
	return err
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.pty.Close()
}
