package chat

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func NewOpenAiClient() openai.Client {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	baseUrl := os.Getenv("OPENROUTER_BASE_URL")
	if baseUrl == "" {
		baseUrl = "https://openrouter.ai/api/v1"
	}

	if apiKey == "" {
		panic("Env variable OPENROUTER_API_KEY not found")
	}

	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseUrl))
}

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func newSkill(content []byte) Skill {

}

func readSkills(path string) []Skill {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading skills %s: %v\n", path, err)
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	skills := make([]Skill, 0, len(entries))

	for _, entry  := range entries {
		if !entry.IsDir() {
			continue
		}

		skill_path := filepath.Join(path, entry.Name(), "SKILL.md")

		content, err := os.ReadFile(skill_path)

		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading skill %s: %v\n", skill_path, err)
		}




		info, err := path.Info()
		info.
	}

	return skills

}

func BuildMessages(prompt string) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 8)
	messages = append(messages, openai.UserMessage(prompt))

	return messages
}
