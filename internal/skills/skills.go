package skills

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Context     string `yaml:"context"`
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

func (s Skill) Fork() bool {
	return strings.ToLower(s.Context) == "fork"
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
