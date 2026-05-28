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

const googleBase = "https://generativelanguage.googleapis.com/v1beta"

type Google struct {
	key    string
	client *http.Client
}

func NewGoogle(key string) *Google {
	return &Google{key: key, client: &http.Client{}}
}

func (g *Google) Models(_ context.Context) ([]string, error) {
	return []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
	}, nil
}

func (g *Google) Stream(ctx context.Context, req common.StreamRequest) (<-chan common.Delta, error) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8192
	}

	payload := map[string]interface{}{
		"contents":         googleMessages(req.Messages),
		"generationConfig": map[string]interface{}{"maxOutputTokens": req.MaxTokens},
	}
	if req.SystemPrompt != "" {
		payload["system_instruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{{"text": req.SystemPrompt}},
		}
	}
	if len(req.Tools) > 0 {
		payload["tools"] = googleToolSpecs(req.Tools)
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", googleBase, req.Model, g.key)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("google: HTTP %d: %s", resp.StatusCode, b)
	}

	ch := make(chan common.Delta, 64)
	go googleSSE(ctx, resp.Body, ch)
	return ch, nil
}

func googleMessages(msgs []common.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case common.RoleUser:
			out = append(out, map[string]interface{}{"role": "user", "parts": googleParts(m.Content)})
		case common.RoleAssistant:
			out = append(out, map[string]interface{}{"role": "model", "parts": googleParts(m.Content)})
		case common.RoleTool:
			parts, ok := m.Content.([]common.Part)
			if !ok {
				break
			}
			var gparts []map[string]interface{}
			for _, p := range parts {
				if p.Type == "tool_result" {
					gparts = append(gparts, map[string]interface{}{
						"function_response": map[string]interface{}{
							"name":     p.Name,
							"response": map[string]interface{}{"content": p.Content},
						},
					})
				}
			}
			if len(gparts) > 0 {
				out = append(out, map[string]interface{}{"role": "user", "parts": gparts})
			}
		}
	}
	return out
}

func googleParts(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case string:
		return []map[string]interface{}{{"text": v}}
	case []common.Part:
		out := make([]map[string]interface{}, 0, len(v))
		for _, p := range v {
			switch p.Type {
			case "text":
				if p.Text != "" {
					out = append(out, map[string]interface{}{"text": p.Text})
				}
			case "tool_use":
				var args interface{}
				_ = json.Unmarshal([]byte(p.Input), &args)
				out = append(out, map[string]interface{}{
					"function_call": map[string]interface{}{"name": p.Name, "args": args},
				})
			}
		}
		return out
	default:
		return []map[string]interface{}{{"text": fmt.Sprint(raw)}}
	}
}

func googleToolSpecs(specs []common.ToolSpec) []map[string]interface{} {
	decls := make([]map[string]interface{}, len(specs))
	for i, s := range specs {
		decls[i] = map[string]interface{}{
			"name":        s.Name,
			"description": s.Description,
			"parameters":  s.InputSchema,
		}
	}
	return []map[string]interface{}{{"function_declarations": decls}}
}

func googleSSE(ctx context.Context, body io.ReadCloser, ch chan<- common.Delta) {
	defer body.Close()
	defer close(ch)

	scan := bufio.NewScanner(body)
	for scan.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var ev struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string `json:"text"`
						FunctionCall *struct {
							Name string                 `json:"name"`
							Args map[string]interface{} `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}

		if json.Unmarshal([]byte(line[6:]), &ev) != nil {
			continue
		}
		if ev.Error != nil {
			push(ctx, ch, common.Delta{Type: common.DeltaError, Error: ev.Error.Message})
			return
		}
		if len(ev.Candidates) == 0 {
			continue
		}
		for _, p := range ev.Candidates[0].Content.Parts {
			if p.Text != "" {
				push(ctx, ch, common.Delta{Type: common.DeltaText, Text: p.Text})
			}
			if p.FunctionCall != nil {
				args, _ := json.Marshal(p.FunctionCall.Args)
				push(ctx, ch, common.Delta{
					Type:     common.DeltaToolCall,
					ToolCall: &common.ToolCall{ID: p.FunctionCall.Name, Name: p.FunctionCall.Name, Input: string(args)},
				})
			}
		}
	}
}
