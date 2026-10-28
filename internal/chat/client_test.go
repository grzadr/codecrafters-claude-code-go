package chat_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// skillEqual compares skills treating nil and empty Content as equal.
func skillEqual(a, b chat.Skill) bool {
	return a.Name == b.Name && a.Description == b.Description && bytes.Equal(a.Body, b.Body)
}

func TestReadSkills(t *testing.T) {
	apple := chat.Skill{
		Name:        "apple",
		Description: "Deploys the apple service to production.",
		Body:        []byte("\nRun the deploy script and report the version that was deployed.\n"),
	}
	grape := chat.Skill{
		Name:        "grape",
		Description: "Runs the grape test suite and reports failures.",
		Body:        []byte("\nRun the tests.\n"),
	}

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
			want: []chat.Skill{
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
			if !slices.EqualFunc(got, tt.want, skillEqual) {
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

const bananaSkill = `---
name: banana
description: Reports the deploy target for the banana service.
---

Respond with exactly one word: blueberry

Do not add any other text, punctuation, or formatting.
`

const cherrySkill = `---
name: cherry
description: Reports the deploy target for the cherry service.
---

Respond with exactly one word: strawberry
`

const echoArgsSkill = `---
name: apple
description: Echoes all arguments.
---

$ARGUMENTS
`

const reverseSkill = `---
name: grape
description: Echoes two arguments in reverse.
---

$1 $0
`

const deploySkill = `---
name: deploy
description: Deploys somewhere.
---

Deploy to $0 in region $ARGUMENTS[1]. Full request was: $ARGUMENTS ($0 again)
`

func TestBuildMessages_Arguments(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{name: "full argument string", prompt: "/apple 4127", want: "4127"},
		{name: "positional in reverse order", prompt: "/grape mango pear", want: "pear mango"},
		{
			name:   "mixed placeholders and repeats",
			prompt: "/deploy staging eu-west",
			want:   "Deploy to staging in region eu-west. Full request was: staging eu-west (staging again)",
		},
		{name: "no arguments become empty strings", prompt: "/grape", want: " "},
		{name: "no arguments with full placeholder", prompt: "/apple", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ".claude/skills/apple/SKILL.md", echoArgsSkill)
			writeFile(t, dir, ".claude/skills/grape/SKILL.md", reverseSkill)
			writeFile(t, dir, ".claude/skills/deploy/SKILL.md", deploySkill)
			t.Chdir(dir)

			msgs := chat.BuildMessages(tt.prompt)
			if got := msgs[len(msgs)-1].OfUser.Content.OfString.Value; got != tt.want {
				t.Errorf("user message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMessages(t *testing.T) {
	t.Run("without skills", func(t *testing.T) {
		t.Chdir(t.TempDir())
		msgs := chat.BuildMessages("hello")
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		if got := msgs[0].OfUser.Content.OfString.Value; got != "hello" {
			t.Errorf("user message = %q, want %q", got, "hello")
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
		if got := msgs[1].OfUser.Content.OfString.Value; got != "hello" {
			t.Errorf("user message = %q, want %q", got, "hello")
		}
	})
}

// assertSkillsListedWithoutBodies checks the system message lists every skill at level 1 only.
func assertSkillsListedWithoutBodies(t *testing.T, system string) {
	t.Helper()
	for _, line := range []string{
		"- banana: Reports the deploy target for the banana service.",
		"- cherry: Reports the deploy target for the cherry service.",
	} {
		if !strings.Contains(system, line) {
			t.Errorf("system message missing %q:\n%s", line, system)
		}
	}
	for _, body := range []string{"blueberry", "strawberry"} {
		if strings.Contains(system, body) {
			t.Errorf("system message leaks skill body %q:\n%s", body, system)
		}
	}
}

func TestBuildMessages_SlashCommand(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string // expected user message content
	}{
		{
			name:   "slash command loads invoked skill body",
			prompt: "/banana",
			want:   "Respond with exactly one word: blueberry\n\nDo not add any other text, punctuation, or formatting.",
		},
		{
			name:   "slash command loads the other skill body",
			prompt: "/cherry",
			want:   "Respond with exactly one word: strawberry",
		},
		{
			name:   "unknown slash command sends raw prompt",
			prompt: "/unknown",
			want:   "/unknown",
		},
		{
			name:   "prompt without slash is sent as is",
			prompt: "banana",
			want:   "banana",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ".claude/skills/banana/SKILL.md", bananaSkill)
			writeFile(t, dir, ".claude/skills/cherry/SKILL.md", cherrySkill)
			t.Chdir(dir)

			msgs := chat.BuildMessages(tt.prompt)
			if len(msgs) != 2 {
				t.Fatalf("got %d messages, want 2", len(msgs))
			}

			if got := msgs[1].OfUser.Content.OfString.Value; got != tt.want {
				t.Errorf("user message = %q, want %q", got, tt.want)
			}

			assertSkillsListedWithoutBodies(t, msgs[0].OfSystem.Content.OfString.Value)
		})
	}
}

func TestParsePrompt(t *testing.T) {
	skills := []chat.Skill{
		{Name: "all", Body: []byte("\n  $ARGUMENTS  \n")},
		{Name: "pos", Body: []byte("$1 $0")},
		{Name: "idx", Body: []byte("$ARGUMENTS[1] $ARGUMENTS[0]")},
		{Name: "mix", Body: []byte("$0|$ARGUMENTS[1]|$ARGUMENTS|$0")},
		{Name: "far", Body: []byte("[$2][$ARGUMENTS[5]][$99999999999999999999]")},
		{Name: "ten", Body: []byte("$10")},
		{Name: "lit", Body: []byte("$ARGUMENTSX $ARGUMENTS_x $ARGUMENTS[x] $x $")},
		{Name: "plain", Body: []byte("no placeholders")},
	}

	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{name: "not a slash command", prompt: "all 1 2", want: "all 1 2"},
		{name: "unknown skill returns prompt", prompt: "/nope a b", want: "/nope a b"},
		{name: "bare slash", prompt: "/", want: "/"},
		{name: "slash with only whitespace", prompt: "/ \t ", want: ""},
		{name: "full arguments with trimmed body", prompt: "/all 4127", want: "4127"},
		{name: "arguments whitespace collapsed", prompt: "/all  a\t b  ", want: "a b"},
		{name: "shorthand positional", prompt: "/pos mango pear", want: "pear mango"},
		{name: "indexed positional", prompt: "/idx mango pear", want: "pear mango"},
		{name: "mixed and repeated", prompt: "/mix a b", want: "a|b|a b|a"},
		{name: "out of range and overflow are empty", prompt: "/far a b", want: "[][][]"},
		{name: "multi-digit index", prompt: "/ten 0 1 2 3 4 5 6 7 8 9 ten", want: "ten"},
		{name: "no arguments empties placeholders", prompt: "/mix", want: "|||"},
		{name: "no arguments full placeholder", prompt: "/all", want: ""},
		{
			name:   "non-placeholders left literal",
			prompt: "/lit a",
			want:   "$ARGUMENTSX $ARGUMENTS_x a[x] $x $",
		},
		{
			name:   "body without placeholders ignores arguments",
			prompt: "/plain a b",
			want:   "no placeholders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chat.ParsePrompt(tt.prompt, skills); got != tt.want {
				t.Errorf("ParsePrompt(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

func TestReadSkills_PathIsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "skills", "not a dir")

	got := chat.ReadSkills(filepath.Join(dir, "skills"))
	if len(got) != 0 {
		t.Errorf("expected no skills when path is a file, got %+v", got)
	}
}
