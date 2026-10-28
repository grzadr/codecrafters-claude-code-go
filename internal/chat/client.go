package chat

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"go.yaml.in/yaml/v3"
)

func NewOpenAiClient() openai.Client {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		panic("Env variable OPENROUTER_API_KEY not found")
	}

	baseURL := cmp.Or(os.Getenv("OPENROUTER_BASE_URL"), "https://openrouter.ai/api/v1")

	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))
}

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Body        []byte
}

const skillHeaderSeparator = "---\n"

func newSkill(content []byte) (Skill, error) {
	sep := []byte(skillHeaderSeparator)
	content, found := bytes.CutPrefix(content, sep)
	if !found {
		return Skill{}, errors.New("skill description not prefixed with `---\n`")
	}

	header, body, found := bytes.Cut(content, sep)
	if !found {
		return Skill{}, errors.New("skill description never closed")
	}

	var s Skill
	if err := yaml.Unmarshal(header, &s); err != nil {
		return Skill{}, err
	}

	s.Body = body

	return s, nil
}

func readSkills(path string) []Skill {
	entries, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "error reading skills %s: %v\n", path, err)
		}

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

		skillPath := filepath.Join(path, entry.Name(), "SKILL.md")

		content, readErr := os.ReadFile(skillPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "error reading skill %s: %v\n", skillPath, readErr)
			continue
		}

		skill, parseErr := newSkill(content)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "error loading skill %s: %v\n", skillPath, parseErr)
			continue
		}

		skills = append(skills, skill)
	}

	return skills
}

const skillsPath = ".claude/skills"

var argumentRegExp = regexp.MustCompile(`\$(?:ARGUMENTS\b(?:\[(\d+)\])?|(\d+))`)

func parsePrompt(prompt string, skills []Skill) string {
	if !strings.HasPrefix(prompt, "/") || len(prompt) == 1 {
		return prompt
	}

	cleared := strings.Fields(prompt[1:])
	if len(cleared) == 0 {
		return ""
	}

	skillName := cleared[0]
	args := cleared[1:]

	for _, s := range skills {
		if s.Name != skillName {
			continue
		}
		body := strings.TrimSpace(string(s.Body))

		return argumentRegExp.ReplaceAllStringFunc(body, func(m string) string {
			sub := argumentRegExp.FindStringSubmatch(m)

			snum := sub[1] + sub[2]

			if snum == "" {
				return strings.Join(args, " ")
			}

			num, err := strconv.Atoi(snum)

			if err != nil || num >= len(args) {
				return ""
			}

			return args[num]
		})
	}

	return prompt
}

func BuildMessages(prompt string) []openai.ChatCompletionMessageParamUnion {
	var messages []openai.ChatCompletionMessageParamUnion

	skills := readSkills(skillsPath)

	if len(skills) > 0 {
		var systemMsg strings.Builder
		systemMsg.WriteString("You have access to the following skills:\n\n")

		for _, skill := range skills {
			fmt.Fprintf(&systemMsg, "- %s: %s\n", skill.Name, skill.Description)
		}

		messages = append(messages, openai.SystemMessage(systemMsg.String()))
	}

	messages = append(messages, openai.UserMessage(parsePrompt(prompt, skills)))

	return messages
}
