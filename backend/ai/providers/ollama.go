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

type Ollama struct {
	baseURL string
	client  *http.Client
}

func NewOllama(baseURL string) *Ollama {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{}}
}

func (o *Ollama) Models(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: cannot reach %s (is Ollama running?): %w", o.baseURL, err)
	}
	defer resp.Body.Close()

	var r struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	out := make([]string, len(r.Models))
	for i, m := range r.Models {
		out[i] = m.Name
	}
	return out, nil
}

func (o *Ollama) Stream(ctx context.Context, req common.StreamRequest) (<-chan common.Delta, error) {
	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": ollamaMessages(req.Messages, req.SystemPrompt),
		"stream":   true,
	}
	if len(req.Tools) > 0 {
		payload["tools"] = ollamaToolSpecs(req.Tools)
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, b)
	}

	ch := make(chan common.Delta, 64)
	go ollamaStream(ctx, resp.Body, ch)
	return ch, nil
}

func ollamaMessages(msgs []common.Message, system string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs)+1)
	if system != "" {
		out = append(out, map[string]interface{}{"role": "system", "content": system})
	}
	for _, m := range msgs {
		switch m.Role {
		case common.RoleUser:
			out = append(out, map[string]interface{}{"role": "user", "content": flatText(m.Content)})
		case common.RoleAssistant:
			parts, ok := m.Content.([]common.Part)
			if !ok {
				out = append(out, map[string]interface{}{"role": "assistant", "content": m.Content})
				break
			}
			var text string
			var calls []map[string]interface{}
			for _, p := range parts {
				switch p.Type {
				case "text":
					text += p.Text
				case "tool_use":
					var args interface{}
					_ = json.Unmarshal([]byte(p.Input), &args)
					calls = append(calls, map[string]interface{}{
						"function": map[string]interface{}{"name": p.Name, "arguments": args},
					})
				}
			}
			msg := map[string]interface{}{"role": "assistant", "content": text}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			out = append(out, msg)
		case common.RoleTool:
			parts, ok := m.Content.([]common.Part)
			if !ok {
				break
			}
			for _, p := range parts {
				if p.Type == "tool_result" {
					out = append(out, map[string]interface{}{"role": "tool", "content": p.Content})
				}
			}
		}
	}
	return out
}

func ollamaToolSpecs(specs []common.ToolSpec) []map[string]interface{} {
	out := make([]map[string]interface{}, len(specs))
	for i, s := range specs {
		out[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        s.Name,
				"description": s.Description,
				"parameters":  s.InputSchema,
			},
		}
	}
	return out
}

func ollamaStream(ctx context.Context, body io.ReadCloser, ch chan<- common.Delta) {
	defer body.Close()
	defer close(ch)

	scan := bufio.NewScanner(body)
	for scan.Scan() {
		if ctx.Err() != nil {
			return
		}
		var ev struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string                 `json:"name"`
						Arguments map[string]interface{} `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if json.Unmarshal(scan.Bytes(), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			push(ctx, ch, common.Delta{Type: common.DeltaError, Error: ev.Error})
			return
		}
		if ev.Message.Content != "" {
			push(ctx, ch, common.Delta{Type: common.DeltaText, Text: ev.Message.Content})
		}
		for _, tc := range ev.Message.ToolCalls {
			args, _ := json.Marshal(tc.Function.Arguments)
			push(ctx, ch, common.Delta{
				Type:     common.DeltaToolCall,
				ToolCall: &common.ToolCall{ID: tc.Function.Name, Name: tc.Function.Name, Input: string(args)},
			})
		}
		if ev.Done {
			return
		}
	}
}
