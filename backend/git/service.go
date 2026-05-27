// Package git provides source control operations
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type FileStatus struct {
	Path      string `json:"path"`
	XY        string `json:"xy"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
}

type CommitEntry struct {
	Hash      string   `json:"hash"`
	ShortHash string   `json:"shortHash"`
	Subject   string   `json:"subject"`
	Author    string   `json:"author"`
	Date      string   `json:"date"`
	Parents   []string `json:"parents"`
	RemoteURL string   `json:"remoteUrl,omitempty"`
}

type Service struct{}

func New() *Service { return &Service{} }

// Branch returns the current branch name, or a short hash when HEAD is detached
func (s *Service) Branch(cwd string) (string, error) {
	out, err := git(cwd, "branch", "--show-current")
	if err != nil {
		hash, err2 := git(cwd, "rev-parse", "--short", "HEAD")
		if err2 != nil {
			return "", err
		}
		return "HEAD detached at " + hash, nil
	}
	return out, nil
}

func (s *Service) ListBranches(cwd string) ([]string, error) {
	out, err := git(cwd, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func (s *Service) Checkout(cwd, branch string) error {
	_, err := git(cwd, "checkout", branch)
	return err
}

func (s *Service) CreateBranch(cwd, branch string) error {
	_, err := git(cwd, "checkout", "-b", branch)
	return err
}

func (s *Service) Status(cwd string) ([]FileStatus, error) {
	out, err := git(cwd, "status", "--porcelain", "-u")
	if err != nil {
		return nil, err
	}

	var files []FileStatus
	for _, line := range splitLines(out) {
		if len(line) < 4 {
			continue
		}
		x, y := line[0], line[1]
		path := line[3:]
		if x == 'R' || y == 'R' {
			if i := strings.Index(path, " -> "); i >= 0 {
				path = path[i+4:]
			}
		}
		files = append(files, FileStatus{
			Path:      path,
			XY:        string([]byte{x, y}),
			Staged:    x != ' ' && x != '?',
			Unstaged:  y != ' ' && y != '?',
			Untracked: x == '?' && y == '?',
		})
	}
	return files, nil
}

func (s *Service) FileDiff(cwd, relPath string) (string, error) {
	return git(cwd, "diff", "--", relPath)
}

func (s *Service) StagedDiff(cwd, relPath string) (string, error) {
	return git(cwd, "diff", "--cached", "--", relPath)
}

func (s *Service) StageFile(cwd, relPath string) error {
	_, err := git(cwd, "add", "--", relPath)
	return err
}

func (s *Service) UnstageFile(cwd, relPath string) error {
	_, err := git(cwd, "restore", "--staged", "--", relPath)
	return err
}

func (s *Service) StageAll(cwd string) error {
	_, err := git(cwd, "add", "-A")
	return err
}

func (s *Service) UnstageAll(cwd string) error {
	_, err := git(cwd, "reset", "HEAD")
	return err
}

func (s *Service) DiscardFile(cwd, relPath string) error {
	if _, err := git(cwd, "restore", "--", relPath); err != nil {
		_, err2 := git(cwd, "clean", "-fd", "--", relPath)
		return err2
	}
	return nil
}

func (s *Service) StageHunk(cwd, relPath, hunkHeader string) error {
	diff, err := s.FileDiff(cwd, relPath)
	if err != nil {
		return err
	}
	return applyHunk(cwd, relPath, diff, hunkHeader, "--cached")
}

func (s *Service) UnstageHunk(cwd, relPath, hunkHeader string) error {
	diff, err := s.StagedDiff(cwd, relPath)
	if err != nil {
		return err
	}
	return applyHunk(cwd, relPath, diff, hunkHeader, "--cached", "--reverse")
}

func (s *Service) DiscardHunk(cwd, relPath, hunkHeader string) error {
	diff, err := s.FileDiff(cwd, relPath)
	if err != nil {
		return err
	}
	return applyHunk(cwd, relPath, diff, hunkHeader, "--reverse")
}

func (s *Service) Commit(cwd, message string) error {
	_, err := git(cwd, "commit", "-m", message)
	return err
}

func (s *Service) AmendCommit(cwd, message string) error {
	_, err := git(cwd, "commit", "--amend", "-m", message)
	return err
}

func (s *Service) Push(cwd string) error {
	_, err := git(cwd, "push")
	return err
}

func (s *Service) PushForce(cwd string) error {
	_, err := git(cwd, "push", "--force-with-lease")
	return err
}

func (s *Service) Log(cwd string, limit int) ([]CommitEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	const str = "%H%x00%h%x00%P%x00%s%x00%an%x00%ad"
	out, err := git(cwd,
		"log",
		"-n", strconv.Itoa(limit),
		"--date=format:%b %d, %Y",
		"--pretty=format:"+str,
	)
	if err != nil {
		return nil, err
	}

	remoteBase, _ := s.remoteBase(cwd)

	var entries []CommitEntry
	for rec := range strings.SplitSeq(out, "\n") {
		if rec == "" {
			continue
		}

		f := strings.Split(rec, "\x00")
		if len(f) < 6 {
			continue
		}

		var parents []string
		if f[2] != "" {
			parents = strings.Fields(f[2])
		}

		ru := ""
		if remoteBase != "" {
			ru = remoteBase + "/commit/" + f[0]
		}

		entries = append(entries, CommitEntry{
			Hash:      f[0],
			ShortHash: f[1],
			Parents:   parents,
			Subject:   f[3],
			Author:    f[4],
			Date:      f[5],
			RemoteURL: ru,
		})
	}
	return entries, nil
}

func (s *Service) StashList(cwd string) ([]string, error) {
	out, err := git(cwd, "stash", "list")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func (s *Service) StashPush(cwd, message string) error {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := git(cwd, args...)
	return err
}

func (s *Service) StashPop(cwd string) error {
	_, err := git(cwd, "stash", "pop")
	return err
}

func (s *Service) RemoteURL(cwd string) (string, error) {
	return git(cwd, "remote", "get-url", "origin")
}

func (s *Service) remoteBase(cwd string) (string, error) {
	u, err := git(cwd, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return normaliseRemote(u), nil
}

func git(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf(
				"git %s: %s",
				strings.Join(args, " "),
				strings.TrimSpace(string(ee.Stderr)),
			)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitStdin(cwd, stdin string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func applyHunk(cwd, relPath, diff, hunkHeader string, flags ...string) error {
	hunk, err := extractHunk(diff, hunkHeader)
	if err != nil {
		return err
	}
	patch := "--- a/" + relPath + "\n+++ b/" + relPath + "\n" + hunk
	return gitStdin(cwd, patch, append([]string{"apply"}, flags...)...)
}

func extractHunk(diff, hunkHeader string) (string, error) {
	var out []string
	capturing := false
	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			capturing = strings.HasPrefix(line, hunkHeader)

			if capturing {
				out = nil
			} else if len(out) > 0 {
				break
			}
		}

		if capturing {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("git: hunk %q not found", hunkHeader)
	}
	return strings.Join(out, "\n") + "\n", nil
}

func normaliseRemote(u string) string {
	if rest, ok := strings.CutPrefix(u, "git@"); ok {
		u = strings.Replace(rest, ":", "/", 1)
		u = "https://" + u
	}
	return strings.TrimSuffix(u, ".git")
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
