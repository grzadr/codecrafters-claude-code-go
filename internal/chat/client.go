package chat

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"go.yaml.in/yaml/v3"
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
	// Content     []byte
}

var skillHeaderSeparator = []byte("---\n")

func newSkill(content []byte) (s Skill, err error) {
	content, found := bytes.CutPrefix(content, skillHeaderSeparator)

	if !found {
		err = fmt.Errorf("skill description not prefixed with `---\n`")
		return
	}
	header, _, found := bytes.Cut(content, skillHeaderSeparator)

	if !found {
		err = fmt.Errorf("skill description never closed")
		return
	}
	err = yaml.Unmarshal([]byte(header), &s)

	return
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

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skill_path := filepath.Join(path, entry.Name(), "SKILL.md")

		content, err := os.ReadFile(skill_path)

		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading skill %s: %v\n", skill_path, err)
			continue
		}

		skill, err := newSkill(content)

		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading skill %s: %v\n", skill_path, err)
			continue
		}

		skills = append(skills, skill)
	}

	return skills
}

const skillsPath = ".claude/skills"

func BuildMessages(prompt string) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 8)

	skills := readSkills(skillsPath)

	if len(skills) > 0 {
		system_msg := "You have access to the following skills:\n\n"

		for _, skill := range skills {
			system_msg = fmt.Sprintf("- %s: %s\n", skill.Name, skill.Description)
		}

		messages = append(messages, openai.SystemMessage(system_msg))
	}

	messages = append(messages, openai.UserMessage(prompt))

	return messages
}
