package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mmatongo/caret/backend/common"
)

const (
	anthropicBase    = "https://api.anthropic.com/v1"
	anthropicVersion = "2023-06-01"
)

type Anthropic struct {
	key    string
	client *http.Client
}

func NewAnthropic(key string) *Anthropic {
	return &Anthropic{key: key, client: &http.Client{}}
}

func (a *Anthropic) Models(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, anthropicBase+"/models", nil)
	a.sign(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: models HTTP %d", resp.StatusCode)
	}

	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}

	out := make([]string, len(r.Data))
	for i, m := range r.Data {
		out[i] = m.ID
	}
	return out, nil
}

func (a *Anthropic) Stream(ctx context.Context, req common.StreamRequest) (<-chan common.Delta, error) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8192
	}

	payload := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   anthropicMessages(req.Messages),
	}
	if req.SystemPrompt != "" {
		payload["system"] = req.SystemPrompt
	}
	if len(req.Tools) > 0 {
		payload["tools"] = anthropicToolSpecs(req.Tools)
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBase+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.sign(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, b)
	}

	ch := make(chan common.Delta, 64)
	go anthropicSSE(ctx, resp.Body, ch)
	return ch, nil
}

func (a *Anthropic) sign(r *http.Request) {
	r.Header.Set("x-api-key", a.key)
	r.Header.Set("anthropic-version", anthropicVersion)
	r.Header.Set("content-type", "application/json")
}

func anthropicMessages(msgs []common.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs))
	var pending []common.Part

	flush := func() {
		if len(pending) == 0 {
			return
		}
		blocks := make([]map[string]interface{}, len(pending))
		for i, p := range pending {
			b := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": p.ID,
				"content":     p.Content,
			}
			if p.IsError {
				b["is_error"] = true
			}
			blocks[i] = b
		}
		out = append(out, map[string]interface{}{"role": "user", "content": blocks})
		pending = nil
	}

	for _, m := range msgs {
		switch m.Role {
		case common.RoleUser:
			flush()
			out = append(out, map[string]interface{}{"role": "user", "content": anthropicContent(m.Content)})
		case common.RoleAssistant:
			flush()
			out = append(out, map[string]interface{}{"role": "assistant", "content": anthropicContent(m.Content)})
		case common.RoleTool:
			if parts, ok := m.Content.([]common.Part); ok {
				for _, p := range parts {
					if p.Type == "tool_result" {
						pending = append(pending, p)
					}
				}
			}
		}
	}
	flush()
	return out
}

func anthropicContent(raw interface{}) interface{} {
	parts, ok := raw.([]common.Part)
	if !ok {
		return raw
	}
	blocks := make([]map[string]interface{}, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": p.Text})
			}
		case "tool_use":
			var input interface{}
			_ = json.Unmarshal([]byte(p.Input), &input)
			blocks = append(blocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    p.ID,
				"name":  p.Name,
				"input": input,
			})
		}
	}
	return blocks
}

func anthropicToolSpecs(specs []common.ToolSpec) []map[string]interface{} {
	out := make([]map[string]interface{}, len(specs))
	for i, s := range specs {
		out[i] = map[string]interface{}{
			"name":         s.Name,
			"description":  s.Description,
			"input_schema": s.InputSchema,
		}
	}
	return out
}

type partialTool struct {
	id   string
	name string
	buf  strings.Builder
}

func anthropicSSE(ctx context.Context, body io.ReadCloser, ch chan<- common.Delta) {
	defer body.Close()
	defer close(ch)

	scan := bufio.NewScanner(body)
	var cur *partialTool

	for scan.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			return
		}

		var ev map[string]interface{}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}

		switch ev["type"] {
		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]interface{})
			if cb["type"] == "tool_use" {
				cur = &partialTool{id: str(cb["id"]), name: str(cb["name"])}
			} else {
				cur = nil
			}

		case "content_block_delta":
			delta, _ := ev["delta"].(map[string]interface{})
			switch delta["type"] {
			case "text_delta":
				push(ctx, ch, common.Delta{Type: common.DeltaText, Text: str(delta["text"])})
			case "input_json_delta":
				if cur != nil {
					cur.buf.WriteString(str(delta["partial_json"]))
				}
			}

		case "content_block_stop":
			if cur != nil {
				push(ctx, ch, common.Delta{
					Type:     common.DeltaToolCall,
					ToolCall: &common.ToolCall{ID: cur.id, Name: cur.name, Input: cur.buf.String()},
				})
				cur = nil
			}

		case "message_stop":
			return

		case "error":
			if errObj, ok := ev["error"].(map[string]interface{}); ok {
				push(ctx, ch, common.Delta{Type: common.DeltaError, Error: str(errObj["message"])})
			}
			return
		}
	}
}

func push(ctx context.Context, ch chan<- common.Delta, d common.Delta) {
	select {
	case ch <- d:
	case <-ctx.Done():
	}
}

func str(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
