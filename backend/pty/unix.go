//go:build !windows

package pty

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func spawn(shell, cwd string, cols, rows uint16, env []string) (*os.File, *exec.Cmd, error) {
	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = env

	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pty: spawn %s: %w", shell, err)
	}
	return master, cmd, nil
}

func resize(master *os.File, cols, rows uint16) error {
	return pty.Setsize(master, &pty.Winsize{Cols: cols, Rows: rows})
}
