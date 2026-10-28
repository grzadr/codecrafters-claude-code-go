package chat

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	Path        string
	Body        []byte
}

func (s *Skill) Msg() string {
	return fmt.Sprintf(
		"Skill: %s (located at %s)\n"+
			"Paths in the instructions below are relative to that folder.\n\n%s",
		s.Name,
		s.Path,
		strings.TrimSpace(string(s.Body)),
	)
}

const skillHeaderSeparator = "---\n"

func newSkill(path string, content []byte) (Skill, error) {
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

	s.Path = path
	s.Body = body

	return s, nil
}

type SkillRegistry map[string]Skill

func readSkills(path string) SkillRegistry {
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

	skills := make(SkillRegistry)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// skillPath := filepath.Join(path, entry.Name())

		mdPath := filepath.Join(path, entry.Name(), "SKILL.md")

		content, readErr := os.ReadFile(mdPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "error reading skill %s: %v\n", mdPath, readErr)
			continue
		}

		skill, parseErr := newSkill(filepath.Join(path, entry.Name()), content)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "error loading skill %s: %v\n", mdPath, parseErr)
			continue
		}

		skills[skill.Name] = skill
	}

	return skills
}

const (
	skillsPath  = ".claude/skills"
	skillPrefix = "/"
)

var argumentRegExp = regexp.MustCompile(`\$(?:ARGUMENTS\b(?:\[(\d+)\])?|(\d+))`)

func getSkillNames(prompt string, skills SkillRegistry) ([]Skill, []string) {
	fields := strings.Fields(prompt)
	last := 0
	found := make([]Skill, 0, len(fields))
	for l, f := range fields {
		if !strings.HasPrefix(f, skillPrefix) || len(f) == 1 {
			break
		}
		skill, ok := skills[f[1:]]
		if !ok {
			break
		}
		found = append(found, skill)
		last = l + 1
	}

	args := fields[last:]

	return found, args
}

func parsePrompt(prompt string, skills SkillRegistry) []string {
	namesSkills, args := getSkillNames(prompt, skills)

	if len(namesSkills) == 0 {
		return []string{prompt}
	}

	userPrompts := make([]string, 0, len(namesSkills))

	for _, s := range namesSkills {
		body := s.Msg()

		out := argumentRegExp.ReplaceAllStringFunc(body, func(m string) string {
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

		userPrompts = append(userPrompts, out)
	}

	return userPrompts
}

func BuildMessages(prompt string) []openai.ChatCompletionMessageParamUnion {
	var messages []openai.ChatCompletionMessageParamUnion

	skills := readSkills(skillsPath)

	if len(skills) > 0 {
		var systemMsg strings.Builder
		systemMsg.WriteString("You have access to the following skills:\n\n")

		for _, s := range slices.Sorted(maps.Keys(skills)) {
			skill := skills[s]
			fmt.Fprintf(&systemMsg, "- %s: %s\n", skill.Name, skill.Description)
		}

		messages = append(messages, openai.SystemMessage(systemMsg.String()))
	}

	for _, m := range parsePrompt(prompt, skills) {
		messages = append(messages, openai.UserMessage(m))
	}

	return messages
}
