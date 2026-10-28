package chat_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codecrafters-io/claude-code-starter-go/internal/chat"
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

// skillMsg is the user message expected when the skill in .claude/skills/<folder> is invoked.
func skillMsg(folder, name, body string) string {
	return "Skill: " + name + " (located at .claude/skills/" + folder + ")\n" +
		"Paths in the instructions below are relative to that folder.\n\n" + body
}

// stripSkillHeader drops the skill location header so tests can compare only the expanded body.
func stripSkillHeader(msg string) string {
	if _, body, ok := strings.Cut(msg, "relative to that folder.\n\n"); ok {
		return body
	}
	return msg
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
		{
			name:   "full argument string",
			prompt: "/apple 4127",
			want:   skillMsg("apple", "apple", "4127"),
		},
		{
			name:   "positional in reverse order",
			prompt: "/grape mango pear",
			want:   skillMsg("grape", "grape", "pear mango"),
		},
		{
			name:   "mixed placeholders and repeats",
			prompt: "/deploy staging eu-west",
			want: skillMsg(
				"deploy",
				"deploy",
				"Deploy to staging in region eu-west. Full request was: staging eu-west (staging again)",
			),
		},
		{
			name:   "no arguments become empty strings",
			prompt: "/grape",
			want:   skillMsg("grape", "grape", " "),
		},
		{
			name:   "no arguments with full placeholder",
			prompt: "/apple",
			want:   skillMsg("apple", "apple", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ".claude/skills/apple/SKILL.md", echoArgsSkill)
			writeFile(t, dir, ".claude/skills/grape/SKILL.md", reverseSkill)
			writeFile(t, dir, ".claude/skills/deploy/SKILL.md", deploySkill)
			t.Chdir(dir)

			reg := skills.InitSkillsRegistry(".claude/skills")
			msgs := chat.BuildMessages(tt.prompt, reg)
			if got := msgs[len(msgs)-1].OfUser.Content.OfString.Value; got != tt.want {
				t.Errorf("user message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMessages(t *testing.T) {
	t.Run("without skills", func(t *testing.T) {
		msgs := chat.BuildMessages("hello", nil)
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
		reg := skills.InitSkillsRegistry(".claude/skills")
		msgs := chat.BuildMessages("hello", reg)
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
			want: skillMsg(
				"banana",
				"banana",
				"Respond with exactly one word: blueberry\n\nDo not add any other text, punctuation, or formatting.",
			),
		},
		{
			name:   "slash command loads the other skill body",
			prompt: "/cherry",
			want:   skillMsg("cherry", "cherry", "Respond with exactly one word: strawberry"),
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

			reg := skills.InitSkillsRegistry(".claude/skills")
			msgs := chat.BuildMessages(tt.prompt, reg)
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
	skills := skills.SkillRegistry{
		"all":   {Name: "all", Body: []byte("\n  $ARGUMENTS  \n")},
		"pos":   {Name: "pos", Body: []byte("$1 $0")},
		"idx":   {Name: "idx", Body: []byte("$ARGUMENTS[1] $ARGUMENTS[0]")},
		"mix":   {Name: "mix", Body: []byte("$0|$ARGUMENTS[1]|$ARGUMENTS|$0")},
		"far":   {Name: "far", Body: []byte("[$2][$ARGUMENTS[5]][$99999999999999999999]")},
		"ten":   {Name: "ten", Body: []byte("$10")},
		"lit":   {Name: "lit", Body: []byte("$ARGUMENTSX $ARGUMENTS_x $ARGUMENTS[x] $x $")},
		"plain": {Name: "plain", Body: []byte("no placeholders")},
	}

	tests := []struct {
		name   string
		prompt string
		want   string // the single expected user prompt
	}{
		{name: "not a slash command", prompt: "all 1 2", want: "all 1 2"},
		{name: "unknown skill returns prompt", prompt: "/nope a b", want: "/nope a b"},
		{name: "bare slash", prompt: "/", want: "/"},
		{name: "slash with only whitespace", prompt: "/ \t ", want: "/ \t "},
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
			got := chat.ParsePrompt(tt.prompt, skills)
			if len(got) != 1 || stripSkillHeader(got[0]) != tt.want {
				t.Errorf("ParsePrompt(%q) = %q, want [%q]", tt.prompt, got, tt.want)
			}
		})
	}
}

const stackAppleSkill = `---
name: apple
description: Reports the deploy target for the apple service.
---

Add this exact line to your response: blueberry-$ARGUMENTS
`

const stackGrapeSkill = `---
name: grape
description: Summarizes recent incidents for the payments team.
---

Add this exact line to your response: cherry-$ARGUMENTS
`

const stackMangoSkill = `---
name: mango
description: Echoes the first two arguments in reverse.
---

kiwi-$1-$0
`

func TestBuildMessages_StackedSkills(t *testing.T) {
	apple := skillMsg("apple", "apple", "Add this exact line to your response: blueberry-")
	grape := skillMsg("grape", "grape", "Add this exact line to your response: cherry-")
	mango := func(body string) string { return skillMsg("mango", "mango", body) }

	tests := []struct {
		name   string
		prompt string
		want   []string // expected user messages, in order
	}{
		{
			name:   "two skills share trailing argument",
			prompt: "/apple /grape 4127",
			want:   []string{apple + "4127", grape + "4127"},
		},
		{
			name:   "order follows the prompt",
			prompt: "/grape /apple 4127",
			want:   []string{grape + "4127", apple + "4127"},
		},
		{
			name:   "non-skill token ends expansion",
			prompt: "/apple 4127 /grape",
			want:   []string{apple + "4127 /grape"},
		},
		{
			name:   "unknown skill ends expansion and stays in arguments",
			prompt: "/apple /pear 4127",
			want:   []string{apple + "/pear 4127"},
		},
		{
			name:   "skill name without slash ends expansion",
			prompt: "/apple grape 4127",
			want:   []string{apple + "grape 4127"},
		},
		{
			name:   "bare slash ends expansion",
			prompt: "/apple / 4127",
			want:   []string{apple + "/ 4127"},
		},
		{
			name:   "stacked skills without arguments",
			prompt: "/apple /grape",
			want:   []string{apple, grape},
		},
		{
			name:   "three skills with positional placeholders",
			prompt: "/apple /grape /mango a b",
			want:   []string{apple + "a b", grape + "a b", mango("kiwi-b-a")},
		},
		{
			name:   "repeated skill is expanded twice",
			prompt: "/apple /apple 1",
			want:   []string{apple + "1", apple + "1"},
		},
		{
			name:   "whitespace between stacked skills collapsed",
			prompt: "/apple \t /grape   x  y",
			want:   []string{apple + "x y", grape + "x y"},
		},
		{
			name:   "unknown first skill sends raw prompt",
			prompt: "/pear /apple 1",
			want:   []string{"/pear /apple 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ".claude/skills/apple/SKILL.md", stackAppleSkill)
			writeFile(t, dir, ".claude/skills/grape/SKILL.md", stackGrapeSkill)
			writeFile(t, dir, ".claude/skills/mango/SKILL.md", stackMangoSkill)
			t.Chdir(dir)

			reg := skills.InitSkillsRegistry(".claude/skills")
			msgs := chat.BuildMessages(tt.prompt, reg)
			if len(msgs) == 0 || msgs[0].OfSystem == nil {
				t.Fatalf("first message is not the system message: %+v", msgs)
			}

			var got []string
			for _, m := range msgs[1:] {
				if m.OfUser == nil {
					t.Fatalf("non-user message after system message: %+v", m)
				}
				got = append(got, m.OfUser.Content.OfString.Value)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("user messages = %q, want %q", got, tt.want)
			}
		})
	}
}

const checksumSkill = `---
name: apple
description: Reports the checksum of the project data file.
---

Run ` + "`scripts/checksum.sh`" + ` using the Bash tool.

Respond with only the value it prints, and nothing else.
`

func TestBuildMessages_SkillLocation(t *testing.T) {
	const checksumBody = "Run `scripts/checksum.sh` using the Bash tool.\n\n" +
		"Respond with only the value it prints, and nothing else."

	tests := []struct {
		name   string
		files  map[string]string // relative to the project root
		prompt string
		want   []string // expected user messages, in order
	}{
		{
			name: "tester example with bundled script",
			files: map[string]string{
				".claude/skills/apple/SKILL.md":            checksumSkill,
				".claude/skills/apple/data.txt":            "random contents",
				".claude/skills/apple/scripts/checksum.sh": "#!/usr/bin/env bash\n",
			},
			prompt: "/apple",
			want: []string{
				"Skill: apple (located at .claude/skills/apple)\n" +
					"Paths in the instructions below are relative to that folder.\n\n" + checksumBody,
			},
		},
		{
			name:   "location is the folder, not the skill name",
			files:  map[string]string{".claude/skills/fruit/SKILL.md": checksumSkill},
			prompt: "/apple",
			want:   []string{skillMsg("fruit", "apple", checksumBody)},
		},
		{
			name:   "header added even when body names no paths",
			files:  map[string]string{".claude/skills/banana/SKILL.md": bananaSkill},
			prompt: "/banana",
			want: []string{
				skillMsg(
					"banana",
					"banana",
					"Respond with exactly one word: blueberry\n\nDo not add any other text, punctuation, or formatting.",
				),
			},
		},
		{
			name: "each stacked skill names its own folder",
			files: map[string]string{
				".claude/skills/apple/SKILL.md":  checksumSkill,
				".claude/skills/cherry/SKILL.md": cherrySkill,
			},
			prompt: "/cherry /apple",
			want: []string{
				skillMsg("cherry", "cherry", "Respond with exactly one word: strawberry"),
				skillMsg("apple", "apple", checksumBody),
			},
		},
		{
			name:   "plain prompt gets no header",
			files:  map[string]string{".claude/skills/apple/SKILL.md": checksumSkill},
			prompt: "run scripts/checksum.sh",
			want:   []string{"run scripts/checksum.sh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeFile(t, dir, rel, content)
			}
			t.Chdir(dir)

			reg := skills.InitSkillsRegistry(".claude/skills")
			var got []string
			for _, m := range chat.BuildMessages(tt.prompt, reg) {
				if m.OfUser != nil {
					got = append(got, m.OfUser.Content.OfString.Value)
				}
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("user messages = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMessages_SkillToolInstructions(t *testing.T) {
	t.Run("system prompt lists skills", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".claude/skills/banana/SKILL.md", bananaSkill)
		writeFile(t, dir, ".claude/skills/cherry/SKILL.md", cherrySkill)
		t.Chdir(dir)

		reg := skills.InitSkillsRegistry(".claude/skills")
		msgs := chat.BuildMessages("What is the deploy target?", reg)
		if len(msgs) == 0 || msgs[0].OfSystem == nil {
			t.Fatalf("first message is not the system message: %+v", msgs)
		}
		system := msgs[0].OfSystem.Content.OfString.Value

		want := "You have access to the following skills:\n\n" +
			"- banana: Reports the deploy target for the banana service.\n" +
			"- cherry: Reports the deploy target for the cherry service."
		if strings.TrimSpace(system) != want {
			t.Errorf("system message = %q, want %q", system, want)
		}
		assertSkillsListedWithoutBodies(t, system)
	})

	t.Run("no skills means no system message", func(t *testing.T) {
		for _, m := range chat.BuildMessages("hello", nil) {
			if m.OfSystem != nil {
				t.Errorf("unexpected system message: %q", m.OfSystem.Content.OfString.Value)
			}
		}
	})
}
