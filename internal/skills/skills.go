package skills

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var subRegExp = regexp.MustCompile(
	`\$\{CLAUDE_SKILL_DIR\}|\$ARGUMENTS(?:\[[^\]\s]*\]?|\w+)?|\$\d\w*`,
)

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Context     string `yaml:"context"`
	Path        string
	Body        string
}

func lookupArg(args []string, idxStr, raw string, matched *bool) string {
	num, err := strconv.ParseUint(idxStr, 10, 0)
	if errors.Is(err, strconv.ErrSyntax) {
		return raw
	}

	*matched = true
	if err != nil || num >= uint64(len(args)) {
		return ""
	}

	return args[num]
}

func (s Skill) Msg(args ...string) string {
	var matched bool
	body := subRegExp.ReplaceAllStringFunc(strings.TrimSpace(s.Body), func(m string) string {
		switch {
		case m == "${CLAUDE_SKILL_DIR}":
			return s.Path
		case m == "$ARGUMENTS":
			matched = true
			return strings.Join(args, " ")
		case strings.HasPrefix(m, "$ARGUMENTS[") && strings.HasSuffix(m, "]"):
			return lookupArg(args, m[len("$ARGUMENTS["):len(m)-1], m, &matched)
		case !strings.HasPrefix(m, "$ARGUMENTS"):
			return lookupArg(args, m[1:], m, &matched)
		default:
			return m
		}
	})

	if !matched && len(args) > 0 {
		body += "\n\nARGUMENTS: " + strings.Join(args, " ")
	}

	return fmt.Sprintf(
		"Skill: %s (located at %s)\n"+
			"Paths in the instructions below are relative to that folder.\n\n%s",
		s.Name,
		s.Path,
		body,
	)
}

func (s Skill) Fork() bool {
	return strings.EqualFold(s.Context, "fork")
}

func newSkill(path string, content []byte) (Skill, error) {
	sep := []byte("---\n")
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
	s.Body = string(body)

	return s, nil
}

type SkillRegistry map[string]Skill

func InitSkillsRegistry(path string) SkillRegistry {
	entries, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "error reading skills %s: %v\n", path, err)
		}

		return nil
	}

	skills := make(SkillRegistry)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(path, entry.Name())
		mdPath := filepath.Join(skillDir, "SKILL.md")

		content, readErr := os.ReadFile(mdPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "error reading skill %s: %v\n", mdPath, readErr)
			continue
		}

		skill, parseErr := newSkill(skillDir, content)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "error loading skill %s: %v\n", mdPath, parseErr)
			continue
		}

		skills[skill.Name] = skill
	}

	return skills
}
