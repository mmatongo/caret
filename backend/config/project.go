package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const projectConfigDir = ".caret"
const projectConfigFile = "config.json"

type ProjectConfig struct {
	AI      ProjectAIConfig   `json:"ai"`
	Ignore  []string          `json:"ignore"`
	Scripts map[string]string `json:"scripts"`
}

type ProjectAIConfig struct {
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	System     string   `json:"system"`
	AllowTools []string `json:"allowTools"`
	MaxTokens  int      `json:"maxTokens"`
}

type ProjectService struct{}

func NewProjectService() *ProjectService { return &ProjectService{} }

func (ps *ProjectService) Get(root string) (ProjectConfig, error) {
	var cfg ProjectConfig
	b, err := os.ReadFile(ps.path(root))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

func (ps *ProjectService) Save(root string, cfg ProjectConfig) error {
	dir := filepath.Join(root, projectConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ps.path(root), b, 0o644)
}

func (ps *ProjectService) path(root string) string {
	return filepath.Join(root, projectConfigDir, projectConfigFile)
}
