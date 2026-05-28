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

type OpenAI struct {
	key     string
	baseURL string
	client  *http.Client
}

func NewOpenAI(key, baseURL string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{key: key, baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{}}
}

func (o *OpenAI) Models(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/models", nil)
	o.sign(req)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: models HTTP %d", resp.StatusCode)
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

func (o *OpenAI) Stream(ctx context.Context, req common.StreamRequest) (<-chan common.Delta, error) {
	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": openAIMessages(req.Messages, req.SystemPrompt),
		"stream":   true,
	}
	if len(req.Tools) > 0 {
		payload["tools"] = openAIToolSpecs(req.Tools)
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	o.sign(httpReq)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, b)
	}

	ch := make(chan common.Delta, 64)
	go openAISSE(ctx, resp.Body, ch)
	return ch, nil
}

func (o *OpenAI) sign(r *http.Request) {
	r.Header.Set("content-type", "application/json")
	if o.key != "" {
		r.Header.Set("authorization", "Bearer "+o.key)
	}
}

func openAIMessages(msgs []common.Message, system string) []map[string]interface{} {
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
			msg := map[string]interface{}{"role": "assistant"}
			var text string
			var calls []map[string]interface{}
			for _, p := range parts {
				switch p.Type {
				case "text":
					text += p.Text
				case "tool_use":
					calls = append(calls, map[string]interface{}{
						"id":   p.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      p.Name,
							"arguments": p.Input,
						},
					})
				}
			}

			if text != "" {
				msg["content"] = text
			} else {
				msg["content"] = nil
			}
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
					out = append(out, map[string]interface{}{
						"role":         "tool",
						"tool_call_id": p.ID,
						"name":         p.Name,
						"content":      p.Content,
					})
				}
			}
		}
	}
	return out
}

func flatText(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	case []common.Part:
		var sb strings.Builder
		for _, p := range v {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	default:
		return fmt.Sprint(raw)
	}
}

func openAIToolSpecs(specs []common.ToolSpec) []map[string]interface{} {
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

type pendingCall struct {
	id   string
	name string
	args strings.Builder
}

func openAISSE(ctx context.Context, body io.ReadCloser, ch chan<- common.Delta) {
	defer body.Close()
	defer close(ch)

	scan := bufio.NewScanner(body)
	calls := map[int]*pendingCall{}

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
			flushCalls(ctx, calls, ch)
			return
		}

		var ev struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil || len(ev.Choices) == 0 {
			continue
		}

		d := ev.Choices[0].Delta
		if d.Content != "" {
			push(ctx, ch, common.Delta{Type: common.DeltaText, Text: d.Content})
		}
		for _, tc := range d.ToolCalls {
			c, ok := calls[tc.Index]
			if !ok {
				c = &pendingCall{}
				calls[tc.Index] = c
			}
			if tc.ID != "" {
				c.id = tc.ID
			}
			if tc.Function.Name != "" {
				c.name = tc.Function.Name
			}
			c.args.WriteString(tc.Function.Arguments)
		}
		if ev.Choices[0].FinishReason == "tool_calls" {
			flushCalls(ctx, calls, ch)
			calls = map[int]*pendingCall{}
		}
	}
}

func flushCalls(ctx context.Context, calls map[int]*pendingCall, ch chan<- common.Delta) {
	for i := 0; i < len(calls); i++ {
		c, ok := calls[i]
		if !ok {
			continue
		}
		push(ctx, ch, common.Delta{
			Type:     common.DeltaToolCall,
			ToolCall: &common.ToolCall{ID: c.id, Name: c.name, Input: c.args.String()},
		})
	}
}
