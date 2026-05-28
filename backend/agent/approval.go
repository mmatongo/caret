package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type ApprovalRequest struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	ToolName  string `json:"toolName"`
	Input     string `json:"input"`
}

type slot struct{ ch chan bool }

type ApprovalManager struct {
	mu      sync.Mutex
	pending map[string]*slot
	seq     atomic.Uint64
}

func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{pending: make(map[string]*slot)}
}

func (m *ApprovalManager) NextID() string {
	return fmt.Sprintf("apr-%d", m.seq.Add(1))
}

func (m *ApprovalManager) Request(ctx context.Context, req ApprovalRequest) (bool, error) {
	s := &slot{ch: make(chan bool, 1)}

	m.mu.Lock()
	m.pending[req.RequestID] = s
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, req.RequestID)
		m.mu.Unlock()
	}()

	select {
	case approved := <-s.ch:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (m *ApprovalManager) Respond(requestID string, approved bool) error {
	m.mu.Lock()
	s, ok := m.pending[requestID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("approval: no pending request %q", requestID)
	}
	s.ch <- approved
	return nil
}
