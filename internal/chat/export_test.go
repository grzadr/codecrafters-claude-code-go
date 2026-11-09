package chat

import (
	"context"

	"github.com/openai/openai-go/v3"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
)

// ParsePrompt exports the unexported parsePrompt function for testing. It runs without a model,
// so it is only meant for skills that do not fork; the error is dropped.
func ParsePrompt(prompt string, registry skills.SkillRegistry) []string {
	got, _ := parsePrompt(context.Background(), openai.Client{}, prompt, registry)
	return got
}
