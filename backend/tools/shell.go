package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mmatongo/caret/backend/common"
)

const (
	shellTimeout = 30 * time.Second
)

type ShellTool struct {
	sessions sync.Map
}

type shellSession struct {
	mu  sync.Mutex
	cwd string
}

func (s *shellSession) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

func (s *shellSession) set(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = dir
}

func NewShellTool() *ShellTool { return &ShellTool{} }

func (t *ShellTool) NeedsApproval() Approval { return ApprovalAlways }

func (t *ShellTool) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name: "run_command",
		Description: `Execute a shell command. Supports 'cd' to change directory persistently across calls within the same session.
Always shown to the user for approval before execution.
Prefer specific commands (go test ./..., npm install) over opaque bash -c strings.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": strProp("Shell command to run"),
				"cwd":     strProp("Working directory override (optional; defaults to session cwd or home)"),
			},
			"required": []string{"command"},
		},
	}
}

func (t *ShellTool) Run(ctx context.Context, sessionID, input string) (string, error) {
	var a struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}

	sess := t.session(sessionID)

	cwd := a.Cwd
	if cwd == "" {
		cwd = sess.get()
	}
	if cwd == "" {
		home, _ := os.UserHomeDir()
		cwd = home
	}

	// Intercept 'cd' so the new directory persists
	if target, ok := parseCD(strings.TrimSpace(a.Command)); ok {
		resolved := resolveDir(cwd, target)
		if _, err := os.Stat(resolved); err != nil {
			return fmt.Sprintf("cd: %s: no such file or directory", resolved), nil
		}
		sess.set(resolved)
		return "cwd: " + resolved, nil
	}

	tctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	sh := userShell()
	cmd := exec.CommandContext(tctx, sh, "-c", a.Command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()

	result := out.String()
	if result == "" && runErr != nil {
		result = runErr.Error()
	} else if runErr != nil {
		result += "\n[exit: " + runErr.Error() + "]"
	}
	return result, nil
}

func (t *ShellTool) ClearSession(sessionID string) {
	t.sessions.Delete(sessionID)
}

func (t *ShellTool) session(id string) *shellSession {
	v, _ := t.sessions.LoadOrStore(id, &shellSession{})
	return v.(*shellSession)
}

func (t *ShellTool) InitSession(sessionID, cwd string) {
	if cwd == "" {
		return
	}
	sess := t.session(sessionID)
	if sess.get() == "" {
		sess.set(cwd)
	}
}

func parseCD(cmd string) (string, bool) {
	if cmd == "cd" {
		home, _ := os.UserHomeDir()
		return home, true
	}
	if strings.HasPrefix(cmd, "cd ") && !strings.ContainsAny(cmd[3:], "&|;") {
		return strings.TrimSpace(cmd[3:]), true
	}
	return "", false
}

func resolveDir(base, target string) string {
	if target == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(target, "~/") {
		home, _ := os.UserHomeDir()
		return home + target[1:]
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(base, target)
}

func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	for _, s := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	return "/bin/sh"
}
