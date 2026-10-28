package chat

import "github.com/codecrafters-io/claude-code-starter-go/internal/skills"

// ParsePrompt exports the unexported parsePrompt function for testing.
func ParsePrompt(prompt string, registry skills.SkillRegistry) []string {
	return parsePrompt(prompt, registry)
}
