// Package agent implements the backend agentic loop
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mmatongo/caret/backend/ai"
	"github.com/mmatongo/caret/backend/common"
	"github.com/mmatongo/caret/backend/tools"
)

const (
	EvDelta    = "agent:delta" // common.Delta (text chunk or tool_call notification)
	EvStart    = "agent:start" // common.ToolCall (tool about to be executed)
	EvResult   = "agent:result"
	EvApproval = "agent:approval"
	EvDone     = "agent:done"
	EvError    = "agent:error"
)

type ToolResultEvent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result string `json:"result"`
	IsErr  bool   `json:"isErr"`
}

type RunOptions struct {
	Provider    string
	Model       string
	Messages    []common.Message
	System      string
	ProjectRoot string
	AllowTools  []string
	MaxTokens   int
	MaxTurns    int
}

type sessionCleaner interface {
	ClearSession(sessionID string)
}

type Agent struct {
	router   *ai.Router
	registry *tools.Registry
	shell    sessionCleaner
	approval *ApprovalManager
	emit     func(event, sessionID string, data interface{})

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func New(
	router *ai.Router,
	registry *tools.Registry,
	shell sessionCleaner,
	approval *ApprovalManager,
	emit func(event, sessionID string, data interface{}),
) *Agent {
	return &Agent{
		router:   router,
		registry: registry,
		shell:    shell,
		approval: approval,
		emit:     emit,
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Run starts the agentic loop in a background goroutine
func (a *Agent) Run(ctx context.Context, sessionID string, opts RunOptions) {
	runCtx, cancel := context.WithCancel(ctx)

	a.mu.Lock()
	a.cancels[sessionID] = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			a.mu.Lock()
			delete(a.cancels, sessionID)
			a.mu.Unlock()
			a.shell.ClearSession(sessionID)
		}()

		if err := a.loop(runCtx, sessionID, opts); err != nil {
			if runCtx.Err() == nil {
				a.emit(EvError, sessionID, err.Error())
			}
			return
		}
		a.emit(EvDone, sessionID, nil)
	}()
}

// Stop cancels a running session
func (a *Agent) Stop(sessionID string) {
	a.mu.Lock()
	cancel, ok := a.cancels[sessionID]
	a.mu.Unlock()
	if ok {
		cancel()
	}
}

func (a *Agent) RespondApproval(requestID string, approved bool) error {
	return a.approval.Respond(requestID, approved)
}

func (a *Agent) loop(ctx context.Context, sessionID string, opts RunOptions) error {
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 30
	}

	system := a.systemPrompt(opts.System, opts.ProjectRoot)
	msgs := make([]common.Message, len(opts.Messages))
	copy(msgs, opts.Messages)
	specs := a.registry.SpecsFor(opts.AllowTools)

	for turn := 0; turn < maxTurns; turn++ {
		if ctx.Err() != nil {
			return nil
		}

		req := common.StreamRequest{
			Model:        opts.Model,
			Messages:     msgs,
			SystemPrompt: system,
			Tools:        specs,
			MaxTokens:    opts.MaxTokens,
		}

		deltas, err := a.router.Stream(ctx, req, opts.Provider)
		if err != nil {
			return fmt.Errorf("stream: %w", err)
		}

		var textBuf strings.Builder
		var calls []common.ToolCall
		var streamErr string

		for d := range deltas {
			switch d.Type {
			case common.DeltaText:
				textBuf.WriteString(d.Text)
				a.emit(EvDelta, sessionID, d)
			case common.DeltaToolCall:
				calls = append(calls, *d.ToolCall)
				a.emit(EvStart, sessionID, d.ToolCall)
			case common.DeltaError:
				streamErr = d.Error
			}
		}

		if streamErr != "" {
			return fmt.Errorf("provider: %s", streamErr)
		}

		if len(calls) == 0 {
			return nil
		}

		msgs = append(msgs, assistantTurn(textBuf.String(), calls))

		results := make([]common.Part, 0, len(calls))
		for _, call := range calls {
			result, isErr := a.execute(ctx, sessionID, call, opts.AllowTools)
			a.emit(EvResult, sessionID, ToolResultEvent{
				ID: call.ID, Name: call.Name, Result: result, IsErr: isErr,
			})
			results = append(results, common.Part{
				Type:    "tool_result",
				ID:      call.ID,
				Name:    call.Name,
				Content: result,
				IsError: isErr,
			})
		}

		msgs = append(msgs, common.Message{Role: common.RoleTool, Content: results})
	}

	return fmt.Errorf("agent: reached %d-turn safety limit", maxTurns)
}

func (a *Agent) execute(ctx context.Context, sessionID string, call common.ToolCall, allowList []string) (string, bool) {
	t, ok := a.registry.Get(call.Name)
	if !ok {
		return fmt.Sprintf("unknown tool %q", call.Name), true
	}

	if len(allowList) > 0 && !contains(allowList, call.Name) {
		return fmt.Sprintf("tool %q is not allowed in this session", call.Name), true
	}

	if t.NeedsApproval() == tools.ApprovalAlways {
		req := ApprovalRequest{
			RequestID: a.approval.NextID(),
			SessionID: sessionID,
			ToolName:  call.Name,
			Input:     call.Input,
		}
		a.emit(EvApproval, sessionID, req)

		approved, err := a.approval.Request(ctx, req)
		if err != nil || !approved {
			return "tool execution denied", false
		}
	}

	result, err := t.Run(ctx, sessionID, call.Input)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	return result, false
}

func assistantTurn(text string, calls []common.ToolCall) common.Message {
	parts := make([]common.Part, 0, 1+len(calls))
	if text != "" {
		parts = append(parts, common.Part{Type: "text", Text: text})
	}
	for _, c := range calls {
		parts = append(parts, common.Part{Type: "tool_use", ID: c.ID, Name: c.Name, Input: c.Input})
	}
	return common.Message{Role: common.RoleAssistant, Content: parts}
}

func (a *Agent) systemPrompt(base, projectRoot string) string {
	if projectRoot == "" {
		return base
	}
	b, err := os.ReadFile(filepath.Join(projectRoot, "CARET.md"))
	if err != nil || len(b) == 0 {
		return base
	}
	mem := "# Project Memory\n\n" + strings.TrimSpace(string(b))
	if base == "" {
		return mem
	}
	return base + "\n\n" + mem
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
