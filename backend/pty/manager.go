// Package pty provides for and manages PTY sessions
package pty

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	seq      atomic.Uint64
	emit     func(event string, data any)
}

func NewManager(emit func(event string, data any)) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		emit:     emit,
	}
}

func (m *Manager) Create(shell, cwd string, cols, rows int, extraEnv []string) (string, error) {
	if shell == "" {
		shell = DetectShell()
	}
	if cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		cwd = home
	}

	env := buildEnv(cwd, extraEnv)

	master, cmd, err := spawn(shell, cwd, uint16(cols), uint16(rows), env)
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("pty-%d", m.seq.Add(1))
	sess := &Session{id: id, pty: master, cmd: cmd}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.readLoop(sess)

	go func() {
		_ = cmd.Wait()
		m.emit("pty:exit:"+id, nil)
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		master.Close()
	}()

	return id, nil
}

func (m *Manager) Write(id, data string) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	return sess.Write([]byte(data))
}

func (m *Manager) Resize(id string, cols, rows int) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	return resize(sess.pty, uint16(cols), uint16(rows))
}

func (m *Manager) Close(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		sess.Close()
	}
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Close(id)
	}
}

func (m *Manager) get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("pty: session %q not found", id)
	}
	return sess, nil
}

func (m *Manager) readLoop(sess *Session) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			m.emit("pty:data:"+sess.id, string(buf[:n]))
		}
		if err != nil {
			if err != io.EOF {
				m.emit("pty:error:"+sess.id, err.Error())
			}
			return
		}
	}
}
