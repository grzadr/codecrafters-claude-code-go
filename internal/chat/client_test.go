package chat_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codecrafters-io/claude-code-starter-go/internal/chat"
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

func TestReadSkills(t *testing.T) {
	apple := chat.Skill{Name: "apple", Description: "Deploys the apple service to production."}
	grape := chat.Skill{Name: "grape", Description: "Runs the grape test suite and reports failures."}

	tests := []struct {
		name  string
		files map[string]string // relative path -> content, written into the skills dir
		want  []chat.Skill
	}{
		{
			name:  "single skill",
			files: map[string]string{"apple/SKILL.md": appleSkill},
			want:  []chat.Skill{apple},
		},
		{
			name: "multiple skills sorted by folder name",
			files: map[string]string{
				"grape/SKILL.md": grapeSkill,
				"apple/SKILL.md": appleSkill,
			},
			want: []chat.Skill{apple, grape},
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
			want: []chat.Skill{apple},
		},
		{
			name: "loose files in skills dir are ignored",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"README.md":      "---\nname: readme\ndescription: not a skill\n---\n",
			},
			want: []chat.Skill{apple},
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
			want: []chat.Skill{apple},
		},
		{
			name: "quoted description containing a colon",
			files: map[string]string{"apple/SKILL.md": `---
name: apple
description: "Deploys: the apple service"
---
`},
			want: []chat.Skill{{Name: "apple", Description: "Deploys: the apple service"}},
		},
		{
			name: "file without frontmatter is skipped",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"plain/SKILL.md": "Just markdown, no frontmatter.\n",
			},
			want: []chat.Skill{apple},
		},
		{
			name: "unterminated frontmatter is skipped",
			files: map[string]string{
				"apple/SKILL.md":  appleSkill,
				"broken/SKILL.md": "---\nname: broken\ndescription: never closed\n",
			},
			want: []chat.Skill{apple},
		},
		{
			name: "invalid yaml is skipped",
			files: map[string]string{
				"apple/SKILL.md": appleSkill,
				"bad/SKILL.md":   "---\nname: [unclosed\n---\n",
			},
			want: []chat.Skill{apple},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeFile(t, dir, rel, content)
			}

			got := chat.ReadSkills(dir)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ReadSkills() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadSkills_MissingDir(t *testing.T) {
	got := chat.ReadSkills(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("expected no skills for missing dir, got %+v", got)
	}
}

func TestNewOpenAiClient(t *testing.T) {
	t.Run("missing api key panics", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "")
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		chat.NewOpenAiClient()
	})

	t.Run("default and custom base url", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "test-key")
		t.Setenv("OPENROUTER_BASE_URL", "")
		_ = chat.NewOpenAiClient()

		t.Setenv("OPENROUTER_BASE_URL", "http://localhost:8080")
		_ = chat.NewOpenAiClient()
	})
}

func TestBuildMessages(t *testing.T) {
	t.Run("without skills", func(t *testing.T) {
		t.Chdir(t.TempDir())
		msgs := chat.BuildMessages("hello")
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
	})

	t.Run("with skills", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".claude/skills/apple/SKILL.md", appleSkill)
		t.Chdir(dir)
		msgs := chat.BuildMessages("hello")
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
	})
}
