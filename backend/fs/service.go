// Package fs provides file-system services
package fs

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	resultNo = 200
)

type FileInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Ext      string `json:"ext"`
	Lang     string `json:"lang"`
}

type SearchResult struct {
	Path  string `json:"path"`
	Score int    `json:"score"`
}

type ContentMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type Service struct {
	watcher *Watcher
}

func NewService(emit func(event string, data any)) *Service {
	w := newWatcher(func(path, op string) {
		payload, _ := json.Marshal(map[string]string{"op": op, "path": path})
		emit("fs:change", string(payload))
	})
	return &Service{watcher: w}
}

func (s *Service) ReadDir(path string) ([]FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	infos := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		infos = append(infos, FileInfo{
			Name:     name,
			Path:     filepath.Join(path, name),
			IsDir:    e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
			Ext:      filepath.Ext(name),
			Lang:     DetectLang(name),
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].IsDir != infos[j].IsDir {
			return infos[i].IsDir
		}
		return strings.ToLower(infos[i].Name) < strings.ToLower(infos[j].Name)
	})
	return infos, nil
}

func (s *Service) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (s *Service) WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (s *Service) Rename(from, to string) error { return os.Rename(from, to) }
func (s *Service) Delete(path string) error     { return os.RemoveAll(path) }
func (s *Service) Mkdir(path string) error      { return os.MkdirAll(path, 0o755) }
func (s *Service) Watch(path string) error      { return s.watcher.Add(path) }
func (s *Service) Unwatch(path string)          { s.watcher.Remove(path) }
func (s *Service) Close()                       { s.watcher.Close() }

func (s *Service) FuzzySearch(query, root string) ([]SearchResult, error) {
	lq := strings.ToLower(query)

	var paths []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			paths = append(paths, rel)
		}
		return nil
	})

	type hit struct {
		path  string
		score int
	}
	var hits []hit
	for _, p := range paths {
		if score := fuzzyScore(lq, strings.ToLower(p)); score > 0 {
			hits = append(hits, hit{p, score})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	out := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, SearchResult{Path: filepath.Join(root, h.path), Score: h.score})
	}
	return out, nil
}

func fuzzyScore(query, target string) int {
	if strings.Contains(target, query) {
		return resultNo - len(target)
	}
	qi := 0
	for _, c := range target {
		if qi < len(query) && rune(query[qi]) == c {
			qi++
		}
	}
	if qi == len(query) {
		return 100 - (len(target) - len(query))
	}
	return 0
}

func (s *Service) SearchContents(query, root string) ([]ContentMatch, error) {
	if query == "" || root == "" {
		return nil, nil
	}
	if _, err := exec.LookPath("rg"); err == nil {
		return rgContentSearch(query, root)
	}
	return walkContentSearch(query, root)
}

func rgContentSearch(query, root string) ([]ContentMatch, error) {
	var out bytes.Buffer
	cmd := exec.Command("rg",
		"--json", "--max-count=5", "--max-filesize=1M",
		"--glob=!.git", "--glob=!node_modules", "--glob=!vendor",
		query, root,
	)
	cmd.Stdout = &out
	cmd.Run()

	var results []ContentMatch
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
		}
	}

	for line := range strings.SplitSeq(out.String(), "\n") {
		if line == "" {
			continue
		}

		if json.Unmarshal([]byte(line), &msg) != nil || msg.Type != "match" {
			continue
		}

		results = append(results, ContentMatch{
			Path: msg.Data.Path.Text,
			Line: msg.Data.LineNumber,
			Text: strings.TrimRight(msg.Data.Lines.Text, "\r\n"),
		})

		if len(results) >= resultNo {
			break
		}
	}
	return results, nil
}

var skipExts = map[string]bool{
	".png": true, ".jpg": true, ".gif": true, ".webp": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true,
	".pdf": true, ".zip": true, ".tar": true, ".exe": true,
	".bin": true, ".so": true, ".dylib": true,
}

func walkContentSearch(query, root string) ([]ContentMatch, error) {
	lq := strings.ToLower(query)
	var results []ContentMatch

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(results) >= resultNo {
			return nil
		}
		if info.IsDir() {
			if skipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 1<<20 || skipExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		hits := 0
		for i, line := range strings.Split(string(b), "\n") {
			if hits >= 5 {
				break
			}
			if strings.Contains(strings.ToLower(line), lq) {
				results = append(results, ContentMatch{
					Path: path,
					Line: i + 1,
					Text: strings.TrimRight(line, "\r"),
				})
				hits++
			}
		}
		return nil
	})

	return results, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "__pycache__",
		".next", "dist", "build", ".cache", "target", ".turbo":
		return true
	}
	return false
}

// Stat returns metadata for a single path.
func (s *Service) Stat(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	name := info.Name()
	return FileInfo{
		Name:     name,
		Path:     path,
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		Modified: info.ModTime().Unix(),
		Ext:      filepath.Ext(name),
		Lang:     DetectLang(name),
	}, nil
}

// keep fmt in scope
