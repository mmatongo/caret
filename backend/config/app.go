// Package config manages application and per-project configuration
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type AppConfig struct {
	AI       AIConfig       `json:"ai"`
	Editor   EditorConfig   `json:"editor"`
	Terminal TerminalConfig `json:"terminal"`
	Keymap   string         `json:"keymap"`
	Theme    string         `json:"theme"`
	FontSize int            `json:"fontSize"`
}

type AIConfig struct {
	DefaultProvider string `json:"defaultProvider"`
	DefaultModel    string `json:"defaultModel"`
	MaxTokens       int    `json:"maxTokens"`
}

type EditorConfig struct {
	TabSize        int  `json:"tabSize"`
	InsertSpaces   bool `json:"insertSpaces"`
	WordWrap       bool `json:"wordWrap"`
	LineNumbers    bool `json:"lineNumbers"`
	MinimapEnabled bool `json:"minimapEnabled"`
	AutoSave       bool `json:"autoSave"`
	FormatOnSave   bool `json:"formatOnSave"`
}

type TerminalConfig struct {
	Shell      string   `json:"shell"`
	FontFamily string   `json:"fontFamily"`
	FontSize   int      `json:"fontSize"`
	Scrollback int      `json:"scrollback"`
	ExtraEnv   []string `json:"extraEnv"`
}

func defaultConfig() AppConfig {
	return AppConfig{
		AI: AIConfig{
			DefaultProvider: "lmstudio",
			DefaultModel:    "qwen2.5-coder-7b-instruct",
			MaxTokens:       8192,
		},
		Editor: EditorConfig{
			TabSize:      4,
			InsertSpaces: true,
			LineNumbers:  true,
			AutoSave:     true,
		},
		Terminal: TerminalConfig{
			Scrollback: 10000,
		},
		Keymap:   "default",
		FontSize: 14,
	}
}

type Service struct {
	mu   sync.RWMutex
	cfg  AppConfig
	path string
}

func NewService() *Service {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "caret", "config.json")
	s := &Service{path: path, cfg: defaultConfig()}
	_ = s.load()
	return s
}

func (s *Service) Get() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Service) Set(cfg AppConfig) error {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return s.save()
}

func (s *Service) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(b, &s.cfg)
}

func (s *Service) save() error {
	s.mu.RLock()
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}
