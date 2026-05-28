package ai

import (
	"fmt"

	"github.com/mmatongo/caret/backend/ai/providers"
	"github.com/mmatongo/caret/backend/common"
	"github.com/mmatongo/caret/backend/keychain"
)

type registryEntry struct {
	requiresKey bool
	build       func(key string) common.Provider
}

var registry = map[string]registryEntry{
	"anthropic":  {true, func(k string) common.Provider { return providers.NewAnthropic(k) }},
	"openai":     {true, func(k string) common.Provider { return providers.NewOpenAI(k, "") }},
	"google":     {true, func(k string) common.Provider { return providers.NewGoogle(k) }},
	"groq":       {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://api.groq.com/openai/v1") }},
	"xai":        {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://api.x.ai/v1") }},
	"cerebras":   {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://api.cerebras.ai/v1") }},
	"openrouter": {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://openrouter.ai/api/v1") }},
	"deepseek":   {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://api.deepseek.com/v1") }},
	"mistral":    {true, func(k string) common.Provider { return providers.NewOpenAI(k, "https://api.mistral.ai/v1") }},
	"lmstudio":   {false, func(_ string) common.Provider { return providers.NewOpenAI("", "http://localhost:1234/v1") }},
	"ollama":     {false, func(_ string) common.Provider { return providers.NewOllama("") }},
}

func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

func KeyedNames() []string {
	out := make([]string, 0, len(registry))
	for name, e := range registry {
		if e.requiresKey {
			out = append(out, name)
		}
	}
	return out
}

func build(name string, kc *keychain.Service) (common.Provider, error) {
	if name == "" {
		name = "lmstudio"
	}
	e, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("ai: unknown provider %q", name)
	}
	var key string
	if e.requiresKey {
		var err error
		key, err = kc.Get(name)
		if err != nil {
			fmt.Printf("ai/registry: keychain miss for %q: %v\n", name, err)
		}
	}
	return e.build(key), nil
}
