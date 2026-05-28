package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mmatongo/caret/backend/common"
)

const (
	resultNo = 200
)

type search struct{}

func Search() Tool { return &search{} }

func (*search) NeedsApproval() Approval { return ApprovalNever }

func (*search) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name:        "search",
		Description: "Search files for a regex pattern using ripgrep (or grep as fallback). Returns path:line:text for each match, up to " + strconv.Itoa(resultNo) + " results.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":          strProp("Regular expression to search for"),
				"path":             strProp("File or directory to search in"),
				"case_insensitive": boolProp("Case-insensitive match (default false)"),
				"include":          strProp(`Restrict to files matching this glob, e.g. "*.go"`),
			},
			"required": []string{"pattern", "path"},
		},
	}
}

type searchMatch struct {
	path string
	line int
	text string
}

func (*search) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		CaseInsensitive bool   `json:"case_insensitive"`
		Include         string `json:"include"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}

	var matches []searchMatch
	var err error

	if _, lookErr := exec.LookPath("rg"); lookErr == nil {
		matches, err = rgSearch(a.Pattern, a.Path, a.CaseInsensitive, a.Include)
	} else {
		matches, err = grepSearch(a.Pattern, a.Path, a.CaseInsensitive)
	}
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "no matches", nil
	}
	var sb strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&sb, "%s:%d: %s\n", m.path, m.line, m.text)
	}
	return sb.String(), nil
}

func rgSearch(pattern, path string, ci bool, include string) ([]searchMatch, error) {
	args := []string{
		"--json", "--max-count=10", "--max-filesize=1M",
		"--glob=!.git", "--glob=!node_modules", "--glob=!vendor",
	}
	if ci {
		args = append(args, "-i")
	}
	if include != "" {
		args = append(args, "--glob="+include)
	}
	args = append(args, pattern, path)

	var out bytes.Buffer
	cmd := exec.Command("rg", args...)
	cmd.Stdout = &out
	cmd.Run()

	var results []searchMatch
	for line := range strings.SplitSeq(out.String(), "\n") {
		if line == "" {
			continue
		}

		var msg struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				Lines struct {
					Text string `json:"text"`
				} `json:"lines"`
				LineNumber int `json:"line_number"`
			} `json:"data"`
		}

		if json.Unmarshal([]byte(line), &msg) != nil || msg.Type != "match" {
			continue
		}

		results = append(results, searchMatch{
			path: msg.Data.Path.Text,
			line: msg.Data.LineNumber,
			text: strings.TrimRight(msg.Data.Lines.Text, "\r\n"),
		})

		if len(results) >= resultNo {
			break
		}
	}
	return results, nil
}

func grepSearch(pattern, path string, ci bool) ([]searchMatch, error) {
	args := []string{"-rn", "--exclude-dir=.git", "--exclude-dir=node_modules", "--exclude-dir=vendor"}
	if ci {
		args = append(args, "-i")
	}
	args = append(args, pattern, path)

	var out bytes.Buffer
	cmd := exec.Command("grep", args...)
	cmd.Stdout = &out
	cmd.Run()

	var results []searchMatch
	for line := range strings.SplitSeq(out.String(), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		var lineNo int
		_, _ = fmt.Sscanf(parts[1], "%d", &lineNo)

		results = append(results, searchMatch{
			path: parts[0],
			line: lineNo,
			text: parts[2],
		})

		if len(results) >= resultNo {
			break
		}
	}
	return results, nil
}
