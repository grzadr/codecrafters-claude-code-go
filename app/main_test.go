package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewMainArgs(t *testing.T) {
	args := newMainArgs([]string{"-p", "hi"})
	if args.Prompt != "hi" {
		t.Fatalf("got %q, want hi", args.Prompt)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	newMainArgs(nil)
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
		stdout := captureStdout(t)
		main()
		if got := stdout(); got != "done" {
			t.Errorf("stdout = %q, want %q", got, "done")
		}
		if !strings.Contains(lastBody, `"content":"hi\n"`) {
			t.Errorf("tool output not sent back to the model, request body: %s", lastBody)
		}
	})

	t.Run("stdout write failure panics", func(t *testing.T) {
		captureStdout(t)
		_ = os.Stdout.Close()
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		main()
	})

	t.Run("api error panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		main()
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

// runSkillScenario runs main in a project with the apple and grape skills. The fake model
// calls Skill with the given arguments once, then answers "done". It returns every request.
//
//nolint:reassign // test overrides os.Args and http.DefaultTransport
func runSkillScenario(t *testing.T, arguments string) []chatRequest {
	t.Helper()
	origArgs, origTransport := os.Args, http.DefaultTransport
	t.Cleanup(func() {
		os.Args, http.DefaultTransport = origArgs, origTransport
	})

	dir := t.TempDir()
	for rel, content := range map[string]string{
		".claude/skills/apple/SKILL.md": migrationSkill,
		".claude/skills/grape/SKILL.md": formatSkill,
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	os.Args = []string{"app", "-p", "What is the database migration status?"}
	t.Setenv("OPENROUTER_API_KEY", "test")

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
// body is the subagent, anything else calls the Skill tool.
func forkModelReply(req chatRequest) string {
	if len(req.Messages) > 0 && strings.Contains(string(req.Messages[0].Content), forkBody) {
		return `{"choices":[{"message":{"role":"assistant","content":"blueberry"}}]}`
	}
	return `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"s1","type":"function",` +
		`"function":{"name":"Skill","arguments":"{\"name\":\"apple\"}"}}]}}]}`
}

// runForkScenario runs main in a project with the forked apple skill and the grape decoy. It
// returns every request and what main printed to stdout.
//
//nolint:reassign // test overrides os.Args and http.DefaultTransport
func runForkScenario(t *testing.T) ([]chatRequest, string) {
	t.Helper()
	origArgs, origTransport := os.Args, http.DefaultTransport
	t.Cleanup(func() {
		os.Args, http.DefaultTransport = origArgs, origTransport
	})

	dir := t.TempDir()
	for rel, content := range map[string]string{
		".claude/skills/apple/SKILL.md": forkSkill,
		".claude/skills/grape/SKILL.md": formatSkill,
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	os.Args = []string{"app", "-p", "Who is on the on-call rotation right now?"}
	t.Setenv("OPENROUTER_API_KEY", "test")
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
	return requests, stdout()
}

func TestMainFunc_ForkSkill(t *testing.T) {
	requests, stdout := runForkScenario(t)
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2 (main, subagent)", len(requests))
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

	t.Run("skill body never enters the main conversation", func(t *testing.T) {
		for _, m := range requests[0].Messages {
			if strings.Contains(string(m.Content), forkBody) {
				t.Errorf("main conversation carries the skill body in a %s message", m.Role)
			}
		}
	})

	// Locked in: the subagent's answer is printed directly as the final answer.
	t.Run("subagent answer is printed", func(t *testing.T) {
		if stdout != "blueberry" {
			t.Errorf("stdout = %q", stdout)
		}
	})
}
