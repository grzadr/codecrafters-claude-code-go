package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewMainArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		want        string
		wantTimeout time.Duration
		wantErr     string // substring; empty means no error
	}{
		{name: "prompt", args: []string{"-p", "hi"}, want: "hi"},
		{
			name:        "prompt with run timeout",
			args:        []string{"-p", "hi", "-timeout", "50ms"},
			want:        "hi",
			wantTimeout: 50 * time.Millisecond,
		},
		{name: "missing prompt", args: nil, wantErr: "prompt must not be empty"},
		{name: "empty prompt", args: []string{"-p", ""}, wantErr: "prompt must not be empty"},
		{name: "unknown flag", args: []string{"-x"}, wantErr: "flag provided but not defined: -x"},
		{
			name:    "unquoted prompt words",
			args:    []string{"-p", "list", "all"},
			wantErr: "quote the prompt",
		},
		// Locked in: extra arguments are reported before the empty prompt.
		{name: "positional prompt", args: []string{"hello"}, wantErr: "quote the prompt"},
		{
			name:    "invalid timeout",
			args:    []string{"-p", "hi", "-timeout", "soon"},
			wantErr: `invalid value "soon" for flag -timeout`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, timeout, err := newMainArgs(tt.args)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			case tt.wantErr != "":
				return
			}
			if got != tt.want || timeout != tt.wantTimeout {
				t.Errorf("got (%q, %v), want (%q, %v)", got, timeout, tt.want, tt.wantTimeout)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       int
		wantStderr string
	}{
		{name: "success", err: nil, want: 0, wantStderr: ""},
		{name: "plain error", err: errors.New("boom"), want: 1, wantStderr: "app: boom\n"},
		{
			name:       "ctrl-c",
			err:        context.Canceled,
			want:       130,
			wantStderr: "app: context canceled\n",
		},
		{
			name:       "wrapped ctrl-c",
			err:        fmt.Errorf("chat completion: %w", context.Canceled),
			want:       130,
			wantStderr: "app: chat completion: context canceled\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := exitCode(tt.err, &stderr); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
			if stderr.String() != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// stubExit replaces [exit] for the rest of the test and returns where the exit code is recorded.
// -1 means exit was never called.
func stubExit(t *testing.T) *int {
	t.Helper()
	code := -1
	orig := exit
	t.Cleanup(func() { exit = orig })
	exit = func(c int) { code = c }
	return &code
}

// captureStdout points [os.Stdout] at a temp file and returns a func reading what was written.
//
//nolint:reassign // test overrides os.Stdout
func captureStdout(t *testing.T) func() string {
	t.Helper()
	orig := os.Stdout
	t.Cleanup(func() { os.Stdout = orig })

	f, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f

	return func() string {
		out, readErr := os.ReadFile(f.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		return string(out)
	}
}

//nolint:reassign // test overrides os.Args and http.DefaultTransport
func TestMainFunc(t *testing.T) {
	origArgs := os.Args
	origTransport := http.DefaultTransport
	t.Cleanup(func() {
		os.Args = origArgs
		http.DefaultTransport = origTransport
	})
	os.Args = []string{"app", "-p", "hi"}
	t.Setenv("OPENROUTER_API_KEY", "test")
	t.Setenv("OPENROUTER_BASE_URL", "")
	t.Chdir(t.TempDir())

	var (
		step     int
		lastBody string
	)
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		step++
		switch step {
		case 1:
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"echo hi\"}"}}]}}]}`,
			)
		case 2, 3:
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`,
			)
		default:
			rec.WriteHeader(http.StatusBadRequest)
		}
		return rec.Result(), nil
	})

	t.Run("tool call then answer is printed", func(t *testing.T) {
		code := stubExit(t)
		stdout := captureStdout(t)
		main()
		if *code != 0 {
			t.Errorf("exit code = %d, want 0", *code)
		}
		if got := stdout(); got != "done" {
			t.Errorf("stdout = %q, want %q", got, "done")
		}
		if !strings.Contains(lastBody, `"content":"hi\n"`) {
			t.Errorf("tool output not sent back to the model, request body: %s", lastBody)
		}
	})

	t.Run("stdout write failure exits 1", func(t *testing.T) {
		code := stubExit(t)
		captureStdout(t)
		_ = os.Stdout.Close()
		main()
		if *code != 1 {
			t.Errorf("exit code = %d, want 1", *code)
		}
	})

	t.Run("api error exits 1", func(t *testing.T) {
		code := stubExit(t)
		main()
		if *code != 1 {
			t.Errorf("exit code = %d, want 1", *code)
		}
	})
}

//nolint:reassign // test overrides http.DefaultTransport
func TestRun(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })
	t.Setenv("OPENROUTER_BASE_URL", "")
	t.Chdir(t.TempDir())

	t.Run("missing api key is an error", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "")
		err := run(t.Context(), []string{"-p", "hi"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY is not set") {
			t.Fatalf("error = %v, want the missing key", err)
		}
	})

	t.Run("bad arguments are an error", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "test")
		err := run(t.Context(), []string{"-p", "list", "all"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "quote the prompt") {
			t.Fatalf("error = %v, want the unquoted prompt error", err)
		}
	})

	t.Run("answer is written to stdout", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "test")
		http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`,
			)
			return rec.Result(), nil
		})
		var stdout bytes.Buffer
		if err := run(t.Context(), []string{"-p", "hi"}, &stdout); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stdout.String() != "done" {
			t.Errorf("stdout = %q, want %q", stdout.String(), "done")
		}
	})

	t.Run("run timeout aborts a hanging request", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "test")
		http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})

		start := time.Now()
		err := run(t.Context(), []string{"-p", "hi", "-timeout", "50ms"}, io.Discard)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("returned after %v, want under 1s", elapsed)
		}
		if err == nil || !strings.Contains(err.Error(), "run timed out after 50ms") {
			t.Fatalf("error = %v, want containing %q", err, "run timed out after 50ms")
		}
		if code := exitCode(err, io.Discard); code != 1 {
			t.Errorf("exitCode = %d, want 1", code)
		}
	})
}

const migrationSkill = "---\nname: apple\n" +
	"description: Use this skill when the user asks for the database migration status.\n---\n\n" +
	"Respond with exactly one word: blueberry\n"

const formatSkill = "---\nname: grape\n" +
	"description: Use this skill when the user asks to format source code.\n---\n\n" +
	"Respond with exactly one word: strawberry\n"

type chatRequest struct {
	Messages []struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"tool_call_id"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

// assertSkillTool checks the request advertises the Skill tool exactly as the stage specifies.
func assertSkillTool(t *testing.T, req chatRequest) {
	t.Helper()
	for _, tool := range req.Tools {
		f := tool.Function
		if f.Name != "Skill" {
			continue
		}
		if f.Description != "Load a skill's instructions into the conversation" {
			t.Errorf("Skill description = %q", f.Description)
		}
		got, _ := json.Marshal(f.Parameters)
		want := `{"properties":{"args":{"description":"Optional arguments for the skill","type":"string"},` +
			`"name":{"description":"The name of the skill to use","type":"string"}},` +
			`"required":["name"],"type":"object"}`
		if string(got) != want {
			t.Errorf("Skill parameters = %s, want %s", got, want)
		}
		return
	}
	t.Errorf("Skill tool not advertised in request tools")
}

func TestMainFunc_SkillTool(t *testing.T) {
	tests := []struct {
		name        string
		arguments   string // Skill tool call arguments sent by the fake model
		wantContain string // the tool result sent back must contain this
		wantAbsent  string // and must not contain this
	}{
		{
			name:        "matching skill body returned",
			arguments:   `{"name":"apple"}`,
			wantContain: "blueberry",
			wantAbsent:  "strawberry",
		},
		{
			name:        "decoy skill body returned only when asked",
			arguments:   `{"name":"grape"}`,
			wantContain: "strawberry",
			wantAbsent:  "blueberry",
		},
		{
			name:        "args are optional and accepted",
			arguments:   `{"name":"apple","args":"prod"}`,
			wantContain: "blueberry",
			wantAbsent:  "strawberry",
		},
		{
			name:        "unknown skill is an error for the model",
			arguments:   `{"name":"pear"}`,
			wantContain: "error: unknown skill: pear",
			wantAbsent:  "berry",
		},
		{
			name:        "invalid arguments are an error for the model",
			arguments:   `{invalid`,
			wantContain: "error:",
			wantAbsent:  "berry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := runSkillScenario(t, tt.arguments)
			if len(requests) != 2 {
				t.Fatalf("got %d requests, want 2", len(requests))
			}
			assertSkillTool(t, requests[0])

			content := lastToolResult(t, requests[1], "s1")
			if !strings.Contains(content, tt.wantContain) {
				t.Errorf("tool result %q missing %q", content, tt.wantContain)
			}
			if strings.Contains(content, tt.wantAbsent) {
				t.Errorf("tool result %q must not contain %q", content, tt.wantAbsent)
			}
		})
	}
}

// setupSkillEnv chdirs into a temp dir populated with files and sets [os.Args], API key, and
// cleanup for [os.Args] and [http.DefaultTransport]. It stubs [exit] and returns where the exit
// code is recorded.
//
//nolint:reassign // test overrides os.Args and http.DefaultTransport
func setupSkillEnv(t *testing.T, prompt string, files map[string]string) *int {
	t.Helper()
	origArgs, origTransport := os.Args, http.DefaultTransport
	t.Cleanup(func() {
		os.Args, http.DefaultTransport = origArgs, origTransport
	})

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	os.Args = []string{"app", "-p", prompt}
	t.Setenv("OPENROUTER_API_KEY", "test")
	return stubExit(t)
}

// runSkillScenario runs main in a project with the apple and grape skills. The fake model
// calls Skill with the given arguments once, then answers "done". It returns every request.
//
//nolint:reassign // test overrides http.DefaultTransport
func runSkillScenario(t *testing.T, arguments string) []chatRequest {
	t.Helper()
	setupSkillEnv(t, "What is the database migration status?", map[string]string{
		".claude/skills/apple/SKILL.md": migrationSkill,
		".claude/skills/grape/SKILL.md": formatSkill,
	})

	call, _ := json.Marshal(arguments)
	var requests []chatRequest
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests = append(requests, req)
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_, _ = fmt.Fprintf(
				rec,
				`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"s1","type":"function","function":{"name":"Skill","arguments":%s}}]}}]}`,
				call,
			)
		} else {
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`,
			)
		}
		return rec.Result(), nil
	})

	main()
	return requests
}

// lastToolResult returns the content of the request's final message, which must be the tool result
// for id.
func lastToolResult(t *testing.T, req chatRequest, id string) string {
	t.Helper()
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "tool" || last.ToolCallID != id {
		t.Fatalf("last message = %+v, want tool result for %s", last, id)
	}
	var content string
	_ = json.Unmarshal(last.Content, &content)
	return content
}

const forkSkill = "---\nname: apple\n" +
	"description: Use this skill when the user asks about the on-call rotation.\n" +
	"context: fork\n---\n\n" +
	"Respond with exactly one word: blueberry\n"

const forkBody = "Respond with exactly one word: blueberry"

// forkedMsg is the main conversation's user message after /apple ran the fork skill.
const forkedMsg = "Skill apple ran in a subagent and returned:\n\nblueberry"

// messageText decodes a message content that was sent as a plain string.
func messageText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("content %s is not a plain string: %v", raw, err)
	}
	return s
}

// forkModelReply answers like the tester's model: a conversation that starts with the forked
// body is the subagent, a main conversation ending in a tool result or in the subagent's answer
// gets the final answer, and anything else calls the Skill tool.
func forkModelReply(req chatRequest) string {
	var last string
	if len(req.Messages) > 0 {
		last = string(req.Messages[len(req.Messages)-1].Content)
	}
	switch {
	case len(req.Messages) > 0 && strings.Contains(string(req.Messages[0].Content), forkBody):
		return `{"choices":[{"message":{"role":"assistant","content":"blueberry"}}]}`
	case len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool",
		strings.Contains(last, "ran in a subagent and returned"):
		return `{"choices":[{"message":{"role":"assistant","content":"The on-call engineer is blueberry."}}]}`
	default:
		return `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"s1","type":"function",` +
			`"function":{"name":"Skill","arguments":"{\"name\":\"apple\"}"}}]}}]}`
	}
}

// runForkScenario runs main with prompt in a project with the forked apple skill and the grape
// decoy. It returns every request, what main printed to stdout, and the exit code.
//
//nolint:reassign // test overrides http.DefaultTransport
func runForkScenario(t *testing.T, prompt string) ([]chatRequest, string, int) {
	t.Helper()
	code := setupSkillEnv(t, prompt, map[string]string{
		".claude/skills/apple/SKILL.md": forkSkill,
		".claude/skills/grape/SKILL.md": formatSkill,
	})
	stdout := captureStdout(t)

	var requests []chatRequest
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req chatRequest
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
			t.Errorf("decode request: %v", decErr)
		}
		requests = append(requests, req)
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(rec, forkModelReply(req))
		return rec.Result(), nil
	})

	main()
	return requests, stdout(), *code
}

func TestMainFunc_ForkSkill(t *testing.T) {
	requests, stdout, code := runForkScenario(t, "Who is on the on-call rotation right now?")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if len(requests) != 3 {
		t.Fatalf("got %d requests, want 3 (main, subagent, main)", len(requests))
	}

	t.Run("subagent gets only the skill message", func(t *testing.T) {
		sub := requests[1]
		if len(sub.Messages) != 1 || sub.Messages[0].Role != "user" {
			t.Fatalf("subagent messages = %+v, want one user message", sub.Messages)
		}
		want := "Skill: apple (located at .claude/skills/apple)\n" +
			"Paths in the instructions below are relative to that folder.\n\n" + forkBody
		if got := messageText(t, sub.Messages[0].Content); got != want {
			t.Errorf("subagent prompt = %q, want %q", got, want)
		}
	})

	t.Run("subagent answer is the tool result", func(t *testing.T) {
		if content := lastToolResult(t, requests[2], "s1"); content != "blueberry" {
			t.Errorf("tool result = %q, want %q", content, "blueberry")
		}
	})

	t.Run("skill body never enters the main conversation", func(t *testing.T) {
		for _, m := range requests[2].Messages {
			if strings.Contains(string(m.Content), forkBody) {
				t.Errorf("main conversation carries the skill body in a %s message", m.Role)
			}
		}
	})

	t.Run("main answer is printed", func(t *testing.T) {
		if stdout != "The on-call engineer is blueberry." {
			t.Errorf("stdout = %q", stdout)
		}
	})
}

func TestMainFunc_SlashForkSkill(t *testing.T) {
	t.Run("slash command runs the skill in a subagent", func(t *testing.T) {
		requests, stdout, code := runForkScenario(t, "/apple")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if len(requests) != 2 {
			t.Fatalf("got %d requests, want 2 (subagent, main)", len(requests))
		}

		sub := requests[0]
		if len(sub.Messages) != 1 || sub.Messages[0].Role != "user" {
			t.Fatalf("subagent messages = %+v, want one user message", sub.Messages)
		}
		want := "Skill: apple (located at .claude/skills/apple)\n" +
			"Paths in the instructions below are relative to that folder.\n\n" + forkBody
		if got := messageText(t, sub.Messages[0].Content); got != want {
			t.Errorf("subagent prompt = %q, want %q", got, want)
		}

		conv := requests[1].Messages
		for _, m := range conv {
			if strings.Contains(string(m.Content), forkBody) {
				t.Errorf("main conversation carries the skill body in a %s message", m.Role)
			}
		}
		last := conv[len(conv)-1]
		if last.Role != "user" || messageText(t, last.Content) != forkedMsg {
			t.Errorf("last main message = %s %s, want user %q", last.Role, last.Content, forkedMsg)
		}

		if stdout != "The on-call engineer is blueberry." {
			t.Errorf("stdout = %q", stdout)
		}
	})
}

//nolint:reassign // test overrides http.DefaultTransport
func TestMainFunc_SlashForkSkillFailure(t *testing.T) {
	code := setupSkillEnv(t, "/apple", map[string]string{
		".claude/skills/apple/SKILL.md": forkSkill,
	})
	stdout := captureStdout(t)
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusBadRequest)
		return rec.Result(), nil
	})

	main()
	if *code != 1 {
		t.Errorf("exit code = %d, want 1", *code)
	}
	if got := stdout(); got != "" {
		t.Errorf("stdout = %q, want nothing", got)
	}
}
