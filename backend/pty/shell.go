package pty

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DetectShell returns the best shell for new PTY sessions
func DetectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}

	switch runtime.GOOS {
	case "darwin":
		for _, s := range []string{"/bin/zsh", "/bin/bash"} {
			if _, err := os.Stat(s); err == nil {
				return s
			}
		}
	case "linux":
		for _, s := range []string{"/bin/bash", "/bin/zsh"} {
			if _, err := os.Stat(s); err == nil {
				return s
			}
		}
	}
	// TODO: https://github.com/UserExistsError/conpty

	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	return "/bin/sh"
}

func buildEnv(cwd string, extra []string) []string {
	base := os.Environ()

	env := make(map[string]string, len(base)+4)
	for _, kv := range base {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}

	if _, ok := env["TERM"]; !ok {
		env["TERM"] = "xterm-256color"
	}
	if _, ok := env["COLORTERM"]; !ok {
		env["COLORTERM"] = "truecolor"
	}
	if cwd != "" {
		env["PWD"] = cwd
	}

	for _, kv := range extra {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
