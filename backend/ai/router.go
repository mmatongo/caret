package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/mmatongo/caret/backend/common"
	"github.com/mmatongo/caret/backend/config"
	"github.com/mmatongo/caret/backend/keychain"
)

type Router struct {
	kc  *keychain.Service
	cfg *config.Service
}

func NewRouter(kc *keychain.Service, cfg *config.Service) *Router {
	return &Router{kc: kc, cfg: cfg}
}

func (r *Router) Stream(ctx context.Context, req common.StreamRequest, providerName string) (<-chan common.Delta, error) {
	p, err := build(providerName, r.kc)
	if err != nil {
		return nil, err
	}
	return p.Stream(ctx, req)
}

func (r *Router) Models(ctx context.Context, providerName string) ([]string, error) {
	p, err := build(providerName, r.kc)
	if err != nil {
		return nil, err
	}
	return p.Models(ctx)
}

func (r *Router) Complete(ctx context.Context, before, after, lang string) (string, error) {
	cfg := r.cfg.Get()
	p, err := build(cfg.AI.DefaultProvider, r.kc)
	if err != nil {
		return "", err
	}

	label := lang
	if label == "" {
		label = "code"
	}

	req := common.StreamRequest{
		Model: cfg.AI.DefaultModel,
		Messages: []common.Message{{
			Role:    common.RoleUser,
			Content: fmt.Sprintf("Code before cursor:\n%s\n<FILL_HERE>\nCode after cursor:\n%s", before, after),
		}},
		SystemPrompt: fitPrompt(label),
		MaxTokens:    512,
	}

	deltas, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for d := range deltas {
		if d.Type == common.DeltaText {
			sb.WriteString(d.Text)
		}
	}
	return cleanFIT(sb.String(), lang), nil
}

func fitPrompt(lang string) string {
	return fmt.Sprintf(
		"You are a code completion engine specialising in %s.\n"+
			"Output ONLY the exact characters to insert at the cursor - no markdown fences,\n"+
			"no language tags, no explanations, no code already present in the context.\n"+
			"Preserve surrounding indentation exactly. If no meaningful completion exists, output nothing.",
		lang,
	)
}

func cleanFIT(s, lang string) string {
	s = strings.TrimRight(s, "\n \t")
	t := strings.TrimSpace(s)

	for _, pfx := range []string{
		"```" + lang + "\n", "```" + lang, "```\n", "```",
		"~~~" + lang + "\n", "~~~" + lang, "~~~\n", "~~~",
	} {
		if strings.HasPrefix(t, pfx) {
			s = t[len(pfx):]
			break
		}
	}
	for _, fence := range []string{"```", "~~~"} {
		if idx := strings.LastIndex(s, fence); idx >= 0 && strings.TrimSpace(s[idx:]) == fence {
			s = s[:idx]
			break
		}
	}

	knownLangs := map[string]bool{
		"go": true, "python": true, "typescript": true, "javascript": true,
		"rust": true, "ruby": true, "java": true, "cpp": true, "c": true,
		"bash": true, "shell": true, "sh": true, "sql": true, "kotlin": true,
	}
	if lines := strings.SplitN(s, "\n", 2); len(lines) == 2 &&
		knownLangs[strings.ToLower(strings.TrimSpace(lines[0]))] {
		s = lines[1]
	}
	return strings.TrimRight(s, "\n ")
}
