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

//nolint:reassign // test overrides os.Args and http.DefaultTransport
func TestMainFunc(t *testing.T) {
	origArgs := os.Args
	origExit := exitFunc
	origTransport := http.DefaultTransport
	t.Cleanup(func() {
		os.Args = origArgs
		exitFunc = origExit
		http.DefaultTransport = origTransport
	})
	os.Args = []string{"app", "-p", "hi"}
	t.Setenv("OPENROUTER_API_KEY", "test")

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
				`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"true\"}"}}]}}]}`,
			)
		case 2:
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`,
			)
		case 3:
			rec.WriteHeader(http.StatusBadRequest)
		case 4:
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"Unknown","arguments":"{}"}}]}}]}`,
			)
		case 5:
			_, _ = fmt.Fprint(
				rec,
				`{"choices":[{"message":{"role":"assistant","content":"recovered"}}]}`,
			)
		case 6:
			_, _ = fmt.Fprint(rec, `{"choices":[]}`)
		}
		return rec.Result(), nil
	})

	// Step 1 -> 2: tool call then assistant message
	main()

	// Step 3: API error
	exited := 0
	exitFunc = func(c int) { exited = c }
	main()
	if exited != 1 {
		t.Fatalf("expected exit 1, got %d", exited)
	}

	// Step 4 -> 5: tool error is sent back to the model and the loop continues
	exited = 0
	main()
	if exited != 0 {
		t.Fatalf("expected no exit after tool error, got %d", exited)
	}
	if !strings.Contains(lastBody, "error: unknown tool: Unknown") {
		t.Fatalf("tool error not sent back to the model, request body: %s", lastBody)
	}

	// Step 6: Empty choices panic
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	main()
}

//nolint:reassign // test overrides os.Args and http.DefaultTransport
func TestMainFunc_TurnCap(t *testing.T) {
	origArgs := os.Args
	origExit := exitFunc
	origTransport := http.DefaultTransport
	t.Cleanup(func() {
		os.Args = origArgs
		exitFunc = origExit
		http.DefaultTransport = origTransport
	})
	os.Args = []string{"app", "-p", "hi"}
	t.Setenv("OPENROUTER_API_KEY", "test")

	// The model never stops calling tools.
	requests := 0
	http.DefaultTransport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests++
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(
			rec,
			`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"Unknown","arguments":"{}"}}]}}]}`,
		)
		return rec.Result(), nil
	})

	exited := 0
	exitFunc = func(c int) { exited = c }
	main()

	if exited != 1 {
		t.Errorf("expected exit 1, got %d", exited)
	}
	if requests != maxTurns {
		t.Errorf("got %d requests, want %d", requests, maxTurns)
	}
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
			name:        "unknown skill returns empty skill message",
			arguments:   `{"name":"pear"}`,
			wantContain: "Skill:  (located at )",
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
	origArgs, origExit, origTransport := os.Args, exitFunc, http.DefaultTransport
	t.Cleanup(func() {
		os.Args, exitFunc, http.DefaultTransport = origArgs, origExit, origTransport
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

	exited := 0
	exitFunc = func(c int) { exited = c }
	main()
	if exited != 0 {
		t.Fatalf("expected no exit, got %d", exited)
	}
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
