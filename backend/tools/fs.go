package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/mmatongo/caret/backend/common"
)

type readFile struct{}

func ReadFile() Tool { return &readFile{} }

func (*readFile) NeedsApproval() Approval { return ApprovalNever }

func (*readFile) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name:        "read_file",
		Description: "Read the full contents of a file from disk.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": strProp("Absolute path to the file")},
			"required":   []string{"path"},
		},
	}
}

func (*readFile) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}
	b, err := os.ReadFile(a.Path)
	return string(b), err
}

type writeFile struct{}

func WriteFile() Tool { return &writeFile{} }

func (*writeFile) NeedsApproval() Approval { return ApprovalAlways }

func (*writeFile) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name:        "write_file",
		Description: "Write content to a file, creating it and any parent directories if necessary. Overwrites existing content.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    strProp("Absolute path to write"),
				"content": strProp("Content to write"),
			},
			"required": []string{"path", "content"},
		},
	}
}

func (*writeFile) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
}

type EditOp struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type multiEdit struct{}

func MultiEdit() Tool { return &multiEdit{} }

func (*multiEdit) NeedsApproval() Approval { return ApprovalAlways }

func (*multiEdit) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name: "multi_edit",
		Description: `Apply multiple precise text substitutions to a file in one atomic operation.
Each edit replaces the first occurrence of 'old' with 'new'.
Edits are applied in order, earlier edits can affect later ones.
'old' must match exactly, including all whitespace and indentation.
Prefer this over write_file when making targeted changes.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": strProp("Absolute path to the file"),
				"edits": map[string]any{
					"type":        "array",
					"description": "Ordered list of find/replace operations",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old": strProp("Exact text to find"),
							"new": strProp("Replacement text"),
						},
						"required": []string{"old", "new"},
					},
				},
			},
			"required": []string{"path", "edits"},
		},
	}
}

func (*multiEdit) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Path  string   `json:"path"`
		Edits []EditOp `json:"edits"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}

	raw, err := os.ReadFile(a.Path)
	if err != nil {
		return "", err
	}
	content := string(raw)

	for i, e := range a.Edits {
		if !strings.Contains(content, e.Old) {
			return "", fmt.Errorf("edit %d: text not found in file:\n%s", i+1, e.Old)
		}
		content = strings.Replace(content, e.Old, e.New, 1)
	}

	if err := os.WriteFile(a.Path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("applied %d edit(s) to %s", len(a.Edits), a.Path), nil
}

type listDir struct{}

func ListDir() Tool { return &listDir{} }

func (*listDir) NeedsApproval() Approval { return ApprovalNever }

func (*listDir) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name:        "list_dir",
		Description: "List files and subdirectories up to 3 levels deep. Skips .git, node_modules, vendor or any other busy folders.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": strProp("Directory to list")},
			"required":   []string{"path"},
		},
	}
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
	".next": true, "dist": true, "build": true, ".cache": true,
}

func (*listDir) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}

	var sb strings.Builder
	err := filepath.Walk(a.Path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(a.Path, p)
		depth := strings.Count(rel, string(os.PathSeparator))
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			if depth > 3 {
				return filepath.SkipDir
			}
		}
		if depth > 3 {
			return nil
		}
		pad := strings.Repeat("  ", depth)
		if info.IsDir() {
			fmt.Fprintf(&sb, "%s%s/\n", pad, info.Name())
		} else {
			fmt.Fprintf(&sb, "%s%s  (%d B)\n", pad, info.Name(), info.Size())
		}
		return nil
	})
	return sb.String(), err
}

type glob struct{}

func Glob() Tool { return &glob{} }

func (*glob) NeedsApproval() Approval { return ApprovalNever }

func (*glob) Spec() common.ToolSpec {
	return common.ToolSpec{
		Name:        "glob",
		Description: `Find files matching a glob pattern within a root directory. Returns relative paths one per line. Skips .git, node_modules, vendor or any other busy folders.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": strProp(`Glob pattern, e.g. "**/*.go" or "src/**/*.ts"`),
				"root":    strProp("Root directory to search from"),
			},
			"required": []string{"pattern", "root"},
		},
	}
}

func (*glob) Run(_ context.Context, _, input string) (string, error) {
	var a struct {
		Pattern string `json:"pattern"`
		Root    string `json:"root"`
	}

	if err := json.Unmarshal([]byte(input), &a); err != nil {
		return "", err
	}

	var matches []string

	err := filepath.WalkDir(a.Root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(a.Root, p)
		if err != nil {
			return nil
		}

		rel = filepath.ToSlash(rel)

		hit, err := doublestar.Match(a.Pattern, rel)
		if err != nil {
			return err
		}

		if hit {
			matches = append(matches, rel)
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "no files matched", nil
	}

	return strings.Join(matches, "\n"), nil
}
