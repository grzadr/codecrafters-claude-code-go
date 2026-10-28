package skills_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
)

// writeFile creates dir/rel (and any parent folders) with the given content.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", rel, err)
	}
}

const appleSkill = `---
name: apple
description: Deploys the apple service to production.
---

Run the deploy script and report the version that was deployed.
`

const grapeSkill = `---
name: grape
description: Runs the grape test suite and reports failures.
---

Run the tests.
`

// skillEqual compares skills treating nil and empty Content as equal.
func skillEqual(a, b skills.Skill) bool {
	return a.Name == b.Name && a.Description == b.Description && bytes.Equal(a.Body, b.Body)
}

func TestInitSkillsRegistry(t *testing.T) {
	apple := skills.Skill{
		Name:        "apple",
		Description: "Deploys the apple service to production.",
		Body:        []byte("\nRun the deploy script and report the version that was deployed.\n"),
	}
	grape := skills.Skill{
		Name:        "grape",
		Description: "Runs the grape test suite and reports failures.",
		Body:        []byte("\nRun the tests.\n"),
	}

	tests := []struct {
		name  string
		files map[string]string // relative path -> content, written into the skills dir
		want  []skills.Skill
	}{
		{
			name:  "single skill",
			files: map[string]string{"apple/SKILL.md": appleSkill},
			want:  []skills.Skill{apple},
		},
		{
			name: "multiple skills keyed by name",
			files: map[string]string{
				"grape/SKILL.md": grapeSkill,
				"apple/SKILL.md": appleSkill,
			},
			want: []skills.Skill{apple, grape},
		},
		{
			name:  "empty skills dir",
			files: map[string]string{},
			want:  nil,
		},
		{
			name: "folder without SKILL.md is skipped",
			files: map[string]string{
				"apple/SKILL.md":  appleSkill,
				"empty/notes.txt": "not a skill",
			},
			want: []skills.Skill{apple},
		},
		{
			name: "loose files in skills dir are ignored",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"README.md":      "---\nname: readme\ndescription: not a skill\n---\n",
			},
			want: []skills.Skill{apple},
		},
		{
			name: "body is not parsed even if it contains --- and yaml-like lines",
			files: map[string]string{"apple/SKILL.md": `---
name: apple
description: Deploys the apple service to production.
---

Body text.

---
name: evil
description: should never be read
`},
			want: []skills.Skill{
				{
					Name:        "apple",
					Description: "Deploys the apple service to production.",
					Body: []byte(
						"\nBody text.\n\n---\nname: evil\ndescription: should never be read\n",
					),
				},
			},
		},
		{
			name: "quoted description containing a colon",
			files: map[string]string{"apple/SKILL.md": `---
name: apple
description: "Deploys: the apple service"
---
`},
			want: []skills.Skill{{Name: "apple", Description: "Deploys: the apple service"}},
		},
		{
			name: "file without frontmatter is skipped",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"plain/SKILL.md": "Just markdown, no frontmatter.\n",
			},
			want: []skills.Skill{apple},
		},
		{
			name: "unterminated frontmatter is skipped",
			files: map[string]string{
				"apple/SKILL.md":  appleSkill,
				"broken/SKILL.md": "---\nname: broken\ndescription: never closed\n",
			},
			want: []skills.Skill{apple},
		},
		{
			name: "invalid yaml is skipped",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"bad/SKILL.md":   "---\nname: [unclosed\n---\n",
			},
			want: []skills.Skill{apple},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeFile(t, dir, rel, content)
			}

			want := skills.SkillRegistry{}
			for _, s := range tt.want {
				want[s.Name] = s
			}

			got := skills.InitSkillsRegistry(dir)
			if !maps.EqualFunc(got, want, skillEqual) {
				t.Errorf("InitSkillsRegistry() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestInitSkillsRegistry_MissingDir(t *testing.T) {
	got := skills.InitSkillsRegistry(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("expected no skills for missing dir, got %+v", got)
	}
}

func TestInitSkillsRegistry_PathIsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skills", "not a dir")

	got := skills.InitSkillsRegistry(filepath.Join(dir, "skills"))
	if len(got) != 0 {
		t.Errorf("expected no skills when path is a file, got %+v", got)
	}
}

func TestSkillMsg(t *testing.T) {
	s := skills.Skill{Name: "apple", Path: "skills/fruit", Body: []byte("\n  Do it.\n\n")}
	want := "Skill: apple (located at skills/fruit)\n" +
		"Paths in the instructions below are relative to that folder.\n\nDo it."
	if got := s.Msg(); got != want {
		t.Errorf("Msg() = %q, want %q", got, want)
	}
}
