// Package tools defines and implements tools, their interface and registry
package tools

import (
	"context"
	"fmt"

	"github.com/mmatongo/caret/backend/common"
)

type Approval int

const (
	// read only tools that run immediately without user input
	ApprovalNever Approval = iota
	// side effecting tools shown to the user before every run
	ApprovalAlways
)

type Tool interface {
	Spec() common.ToolSpec
	NeedsApproval() Approval
	Run(ctx context.Context, sessionID, input string) (string, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	name := t.Spec().Name
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tools: duplicate name %q", name))
	}
	r.tools[name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	return out
}

func (r *Registry) Specs() []common.ToolSpec {
	out := make([]common.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Spec())
	}
	return out
}

func (r *Registry) SpecsFor(allow []string) []common.ToolSpec {
	if len(allow) == 0 {
		return r.Specs()
	}
	set := make(map[string]bool, len(allow))
	for _, n := range allow {
		set[n] = true
	}
	out := make([]common.ToolSpec, 0, len(allow))
	for n, t := range r.tools {
		if set[n] {
			out = append(out, t.Spec())
		}
	}
	return out
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
