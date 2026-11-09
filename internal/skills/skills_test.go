package skills_test

import (
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

// skillEqual compares skills ignoring the runtime directory Path.
func skillEqual(a, b skills.Skill) bool {
	return a.Name == b.Name && a.Description == b.Description && a.Body == b.Body
}

func TestInitSkillsRegistry(t *testing.T) {
	apple := skills.Skill{
		Name:        "apple",
		Description: "Deploys the apple service to production.",
		Body:        "\nRun the deploy script and report the version that was deployed.\n",
	}
	grape := skills.Skill{
		Name:        "grape",
		Description: "Runs the grape test suite and reports failures.",
		Body:        "\nRun the tests.\n",
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
					Body:        "\nBody text.\n\n---\nname: evil\ndescription: should never be read\n",
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
		{
			name: "--- inside a header value does not close the frontmatter",
			files: map[string]string{
				"apple/SKILL.md": "---\nname: apple\ndescription: Separator ---\n---\nbody\n",
			},
			want: []skills.Skill{{Name: "apple", Description: "Separator ---", Body: "body\n"}},
		},
		{
			name: "--- inside a header comment does not close the frontmatter",
			files: map[string]string{
				"apple/SKILL.md": "---\n# ---------\nname: apple\ndescription: d\n---\nbody\n",
			},
			want: []skills.Skill{{Name: "apple", Description: "d", Body: "body\n"}},
		},
		{
			name: "indented --- in a block scalar does not close the frontmatter",
			files: map[string]string{
				"apple/SKILL.md": "---\nname: apple\ndescription: |\n  first\n  ---\n  last\n---\nbody\n",
			},
			want: []skills.Skill{
				{Name: "apple", Description: "first\n---\nlast", Body: "body\n"},
			},
		},
		{
			name:  "empty frontmatter closes and falls back to the folder name",
			files: map[string]string{"plain/SKILL.md": "---\n---\nbody\n"},
			want:  []skills.Skill{{Name: "plain", Body: "body\n"}},
		},
		{
			// Locked in: CRLF is normalised to LF in the body too.
			name: "CRLF line endings",
			files: map[string]string{
				"apple/SKILL.md": "---\r\nname: apple\r\ndescription: d\r\n---\r\nbody\r\n",
			},
			want: []skills.Skill{{Name: "apple", Description: "d", Body: "body\n"}},
		},
		{
			name: "UTF-8 byte order mark",
			files: map[string]string{
				"apple/SKILL.md": "\xEF\xBB\xBF---\nname: apple\ndescription: d\n---\nbody\n",
			},
			want: []skills.Skill{{Name: "apple", Description: "d", Body: "body\n"}},
		},
		{
			name:  "frontmatter only, without a final newline",
			files: map[string]string{"apple/SKILL.md": "---\nname: apple\ndescription: d\n---"},
			want:  []skills.Skill{{Name: "apple", Description: "d"}},
		},
		{
			name:  "skill without a name uses its folder name",
			files: map[string]string{"deploy/SKILL.md": "---\ndescription: Deploys.\n---\nbody\n"},
			want:  []skills.Skill{{Name: "deploy", Description: "Deploys.", Body: "body\n"}},
		},
		{
			name: "duplicate names keep the first folder",
			files: map[string]string{
				"dupA/SKILL.md": "---\nname: dup\ndescription: first\n---\n",
				"dupB/SKILL.md": "---\nname: dup\ndescription: second\n---\n",
			},
			want: []skills.Skill{{Name: "dup", Description: "first"}},
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

func TestInitSkillsRegistry_Symlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "real/apple/SKILL.md", appleSkill)
	dir := filepath.Join(root, "skills")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "real", "apple"),
		filepath.Join(dir, "apple"),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "missing"),
		filepath.Join(dir, "dangling"),
	); err != nil {
		t.Fatal(err)
	}

	got := skills.InitSkillsRegistry(dir)
	if len(got) != 1 || got["apple"].Description != "Deploys the apple service to production." {
		t.Errorf("InitSkillsRegistry() = %+v, want only the symlinked apple skill", got)
	}
}

func TestSkillMsg(t *testing.T) {
	const header = "Skill: apple (located at skills/fruit)\n" +
		"Paths in the instructions below are relative to that folder.\n\n"

	tests := []struct {
		name string
		body string
		args string
		want string
	}{
		{
			name: "trims body whitespace",
			body: "\n  Do it.\n\n",
			want: "Do it.",
		},
		{
			name: "substitutes all placeholders and skill dir",
			body: "${CLAUDE_SKILL_DIR}|$ARGUMENTS|$ARGUMENTS[1]|$0|$2",
			args: "a b",
			want: "skills/fruit|a b|b|a|",
		},
		{
			name: "leaves invalid placeholders literal",
			body: "$0 $ARGUMENTSX $ARGUMENTS[x] $ARGUMENTS[0 $0x $",
			args: "a",
			want: "a $ARGUMENTSX $ARGUMENTS[x] $ARGUMENTS[0 $0x $",
		},
		{
			name: "appends arguments when no argument placeholder matched",
			body: "Run ${CLAUDE_SKILL_DIR}/run.sh",
			args: "a b",
			want: "Run skills/fruit/run.sh\n\nARGUMENTS: a b",
		},
		{
			name: "full arguments keep inner whitespace, outer is trimmed",
			body: "[$ARGUMENTS] $1 $0",
			args: "  a\n    b\t c  \n",
			want: "[a\n    b\t c] b a",
		},
		{
			name: "appended arguments keep inner whitespace",
			body: "plain",
			args: "line one\n  line two",
			want: "plain\n\nARGUMENTS: line one\n  line two",
		},
		{
			name: "whitespace-only arguments count as none",
			body: "plain",
			args: " \n\t ",
			want: "plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := skills.Skill{Name: "apple", Path: "skills/fruit", Body: tt.body}
			if got := s.Msg(tt.args); got != header+tt.want {
				t.Errorf("Msg() = %q, want %q", got, header+tt.want)
			}
		})
	}
}

func TestSkillFork(t *testing.T) {
	tests := []struct {
		context string
		want    bool
	}{
		{context: "", want: false},
		{context: "fork", want: true},
		{context: "FORK", want: true},   // locked in: comparison is case-insensitive
		{context: " fork", want: false}, // locked in: surrounding whitespace is not trimmed
		{context: "inline", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.context, func(t *testing.T) {
			s := skills.Skill{Context: tt.context}
			if got := s.Fork(); got != tt.want {
				t.Errorf("Fork() with context %q = %v, want %v", tt.context, got, tt.want)
			}
		})
	}
}

func TestInitSkillsRegistry_Context(t *testing.T) {
	dir := t.TempDir()
	writeFile(
		t,
		dir,
		"apple/SKILL.md",
		"---\nname: apple\ndescription: d\ncontext: fork\n---\nbody\n",
	)

	got := skills.InitSkillsRegistry(dir)
	if s := got["apple"]; s.Context != "fork" || !s.Fork() {
		t.Errorf("apple context = %q, Fork() = %v, want %q and true", s.Context, s.Fork(), "fork")
	}
}
