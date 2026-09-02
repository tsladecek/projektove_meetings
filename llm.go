package projektovemeeting

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/openai"
)

type LLMProvider string

const (
	LLMProviderGoogle    = "googleai"
	LLMProviderOpenAI    = "openai"
	LLMProviderAnthropic = "anthropic"
)

func initBackend(ctx context.Context, provider LLMProvider, model, token string) (llms.Model, error) {
	switch provider {
	case "openai":
		return openai.New(openai.WithModel(model), openai.WithToken(token))
	case "anthropic":
		return anthropic.New(anthropic.WithModel(model))
	case "googleai":
		return googleai.New(ctx, googleai.WithDefaultModel(model), googleai.WithAPIKey(token))
	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
}

type LLMInferer struct {
	model llms.Model
}

func NewLLM(provider LLMProvider, model, token string) (LLM, error) {
	m, err := initBackend(context.Background(), provider, model, token)
	if err != nil {
		return LLMInferer{}, fmt.Errorf("when initializing llm provider: %w", err)
	}
	return LLMInferer{model: m}, nil
}

func (l LLMInferer) Infer(ctx context.Context, prompt string) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, l.model, prompt, llms.WithJSONMode(), llms.WithTemperature(0))
}
