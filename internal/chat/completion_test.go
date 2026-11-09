package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	"github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"tool_call_id"`
	} `json:"messages"`
}

// text decodes the content of message i, which must have been sent as a plain string.
func (r chatRequest) text(t *testing.T, i int) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(r.Messages[i].Content, &s); err != nil {
		t.Fatalf("message %d content %s is not a plain string: %v", i, r.Messages[i].Content, err)
	}
	return s
}

// lastToolResult returns the content of the request's final message, which must be the tool
// result for id.
func (r chatRequest) lastToolResult(t *testing.T, id string) string {
	t.Helper()
	i := len(r.Messages) - 1
	if r.Messages[i].Role != "tool" || r.Messages[i].ToolCallID != id {
		t.Fatalf("last message = %+v, want tool result for %s", r.Messages[i], id)
	}
	return r.text(t, i)
}

// checkErr fails unless err matches wantErr: empty means no error, otherwise a substring.
func checkErr(t *testing.T, err error, wantErr string) {
	t.Helper()
	switch {
	case wantErr == "" && err != nil:
		t.Fatalf("unexpected error: %v", err)
	case wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)):
		t.Fatalf("error = %v, want containing %q", err, wantErr)
	}
}

// reply is one scripted model response. A zero status means 200.
type reply struct {
	status int
	body   string
}

func contentReply(content string) reply {
	return reply{
		body: fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}]}`, content),
	}
}

func toolCallReply(id, name, args string) reply {
	return reply{body: fmt.Sprintf(
		`{"choices":[{"message":{"role":"assistant","tool_calls":[`+
			`{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`,
		id, name, args,
	)}
}

// fakeModel returns a client whose n-th request gets script(n) and a pointer to every request
// it received.
func fakeModel(t *testing.T, script func(n int) reply) (openai.Client, *[]chatRequest) {
	t.Helper()
	var requests []chatRequest
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests = append(requests, req)

		rep := script(len(requests))
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		if rep.status != 0 {
			rec.WriteHeader(rep.status)
		}
		_, _ = fmt.Fprint(rec, rep.body)
		return rec.Result(), nil
	})

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL("http://model.test"),
		option.WithMaxRetries(0),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	)
	return client, &requests
}

// sequence scripts replies in order; requests past the end get the last reply.
func sequence(replies ...reply) func(int) reply {
	return func(n int) reply { return replies[min(n, len(replies))-1] }
}

func TestSendCompletion(t *testing.T) {
	tests := []struct {
		name         string
		script       func(int) reply
		want         string
		wantErr      string // substring; empty means no error
		wantRequests int
		wantTool     string // substring of the last tool result sent back; empty means unchecked
	}{
		{
			name:         "plain answer",
			script:       sequence(contentReply("done")),
			want:         "done",
			wantRequests: 1,
		},
		{
			name:         "api error",
			script:       sequence(reply{status: http.StatusBadRequest, body: `{}`}),
			wantErr:      "400 Bad Request",
			wantRequests: 1,
		},
		{
			name:         "empty choices",
			script:       sequence(reply{body: `{"choices":[]}`}),
			wantErr:      "no choices in response",
			wantRequests: 1,
		},
		{
			name: "tool output is sent back",
			script: sequence(
				toolCallReply("t1", "Bash", `{"command":"echo hi"}`),
				contentReply("done"),
			),
			want:         "done",
			wantRequests: 2,
			wantTool:     "hi\n",
		},
		{
			name: "tool error is sent back and the loop continues",
			script: sequence(
				toolCallReply("t1", "Unknown", `{}`),
				contentReply("recovered"),
			),
			want:         "recovered",
			wantRequests: 2,
			wantTool:     "error: unknown tool: Unknown\n",
		},
		{
			name:         "gives up after the turn cap",
			script:       sequence(toolCallReply("t1", "Unknown", `{}`)),
			wantErr:      "gave up after 50 turns",
			wantRequests: 50,
		},
		{
			name: "failing command keeps its output and appends the error",
			script: sequence(
				toolCallReply("t1", "Bash", `{"command":"echo out; echo err >&2; exit 3"}`),
				contentReply("done"),
			),
			want:         "done",
			wantRequests: 2,
			wantTool:     "out\nerr\nerror: command exited with code 3: exit status 3\n",
		},
		{
			name: "refusal is an error",
			script: sequence(reply{
				body: `{"choices":[{"message":{"role":"assistant","content":"","refusal":"no"}}]}`,
			}),
			wantErr:      "model refused: no",
			wantRequests: 1,
		},
		{
			name: "empty reply is an error",
			script: sequence(reply{
				body: `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}]}`,
			}),
			wantErr:      `empty reply (finish_reason "length")`,
			wantRequests: 1,
		},
		{
			name: "array content prints only the text parts",
			script: sequence(reply{
				body: `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[` +
					`{"type":"reasoning","text":"thinking"},` +
					`{"type":"text","text":"Verdict: OK\n"},` +
					`{"type":"text","text":"- detail"}]}}]}`,
			}),
			want:         "Verdict: OK\n- detail",
			wantRequests: 1,
		},
		{
			name: "array content without text parts is an empty reply",
			script: sequence(reply{
				body: `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[` +
					`{"type":"reasoning","text":"thinking"}]}}]}`,
			}),
			wantErr:      `empty reply (finish_reason "stop")`,
			wantRequests: 1,
		},
		{
			name:         "string content that looks like an array is kept verbatim",
			script:       sequence(contentReply(`[{"type":"text","text":"x"}]`)),
			want:         `[{"type":"text","text":"x"}]`,
			wantRequests: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, requests := fakeModel(t, tt.script)
			msgs := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}

			got, err := chat.SendCompletion(t.Context(), client, msgs, tools.NewToolsRegistry(nil))

			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if len(*requests) != tt.wantRequests {
				t.Fatalf("got %d requests, want %d", len(*requests), tt.wantRequests)
			}
			if (*requests)[0].Model != "anthropic/claude-haiku-4.5" {
				t.Errorf("model = %q", (*requests)[0].Model)
			}
			if tt.wantTool != "" {
				last := (*requests)[len(*requests)-1]
				if content := last.lastToolResult(t, "t1"); content != tt.wantTool {
					t.Errorf("tool result = %q, want %q", content, tt.wantTool)
				}
			}
		})
	}
}

func TestSendCompletion_ForkSkill(t *testing.T) {
	apple := skills.Skill{
		Name:    "apple",
		Context: "fork",
		Path:    ".claude/skills/apple",
		Body:    "\nRespond with exactly one word: blueberry\n",
	}
	reg := tools.NewToolsRegistry(skills.SkillRegistry{"apple": apple})
	callSkill := toolCallReply("s1", "Skill", `{"name":"apple"}`)

	t.Run("subagent answer is sent back as the tool result", func(t *testing.T) {
		client, requests := fakeModel(t, sequence(
			callSkill,
			contentReply("blueberry"),
			contentReply("The on-call engineer is blueberry."),
		))
		msgs := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("who is on call?")}

		got, err := chat.SendCompletion(t.Context(), client, msgs, reg)
		checkErr(t, err, "")
		if got != "The on-call engineer is blueberry." {
			t.Errorf("got %q, want the main conversation's answer", got)
		}
		if len(*requests) != 3 {
			t.Fatalf("got %d requests, want 3 (main, subagent, main)", len(*requests))
		}

		sub := (*requests)[1]
		if len(sub.Messages) != 1 || sub.Messages[0].Role != "user" {
			t.Fatalf("subagent messages = %+v, want one user message", sub.Messages)
		}
		if prompt := sub.text(t, 0); prompt != apple.Msg("") {
			t.Errorf("subagent prompt = %q, want %q", prompt, apple.Msg(""))
		}

		if content := (*requests)[2].lastToolResult(t, "s1"); content != "blueberry" {
			t.Errorf("tool result = %q, want %q", content, "blueberry")
		}
	})

	t.Run("subagent failure is sent back as a tool error", func(t *testing.T) {
		client, requests := fakeModel(t, sequence(
			callSkill,
			reply{status: http.StatusBadRequest, body: `{}`},
			contentReply("recovered"),
		))
		msgs := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("who is on call?")}

		got, err := chat.SendCompletion(t.Context(), client, msgs, reg)
		checkErr(t, err, "")
		if got != "recovered" {
			t.Errorf("got %q, want %q", got, "recovered")
		}
		if len(*requests) != 3 {
			t.Fatalf("got %d requests, want 3 (main, subagent, main)", len(*requests))
		}

		if content := (*requests)[2].lastToolResult(
			t,
			"s1",
		); !strings.Contains(
			content,
			"400 Bad Request",
		) {
			t.Errorf("tool result = %q, want the subagent error", content)
		}
	})
}

func TestSendCompletion_CancelStopsQueuedToolCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queued.txt")
	writeArgs, err := json.Marshal(map[string]string{"file_path": path, "content": "x"})
	if err != nil {
		t.Fatal(err)
	}
	// One assistant turn with two tool calls: a slow Bash, then a Write queued behind it.
	body := fmt.Sprintf(
		`{"choices":[{"message":{"role":"assistant","tool_calls":[`+
			`{"id":"t1","type":"function","function":{"name":"Bash","arguments":%q}},`+
			`{"id":"t2","type":"function","function":{"name":"Write","arguments":%q}}]}}]}`,
		`{"command":"sleep 2"}`, writeArgs,
	)
	client, requests := fakeModel(t, sequence(reply{body: body}, contentReply("done")))
	msgs := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err = chat.SendCompletion(ctx, client, msgs, tools.NewToolsRegistry(nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("queued Write ran after cancel (stat error: %v)", statErr)
	}
	if len(*requests) != 1 {
		t.Errorf("got %d requests, want 1", len(*requests))
	}
}

// userTexts returns the content of every user message after the leading system message, failing
// on any other role.
func userTexts(t *testing.T, msgs []openai.ChatCompletionMessageParamUnion) []string {
	t.Helper()
	if len(msgs) == 0 || msgs[0].OfSystem == nil {
		t.Fatalf("first message is not the system message: %+v", msgs)
	}
	var got []string
	for _, m := range msgs[1:] {
		if m.OfUser == nil {
			t.Fatalf("non-user message after the system message: %+v", m)
		}
		got = append(got, m.OfUser.Content.OfString.Value)
	}
	return got
}

func TestBuildMessages_SlashForkSkill(t *testing.T) {
	apple := skills.Skill{
		Name:        "apple",
		Description: "Reports the on-call engineer.",
		Context:     "fork",
		Path:        ".claude/skills/apple",
		Body:        "\nRespond with exactly one word: blueberry\n",
	}
	grape := skills.Skill{
		Name:        "grape",
		Description: "Inline helper.",
		Path:        ".claude/skills/grape",
		Body:        "Echo $ARGUMENTS",
	}
	reg := skills.SkillRegistry{"apple": apple, "grape": grape}
	const forked = "Skill apple ran in a subagent and returned:\n\nblueberry"

	t.Run("subagent gets only the skill body and its answer is the message", func(t *testing.T) {
		client, requests := fakeModel(t, sequence(contentReply("blueberry")))

		msgs, err := chat.BuildMessages(t.Context(), client, "/apple prod", reg)
		checkErr(t, err, "")
		if len(*requests) != 1 {
			t.Fatalf("got %d requests, want 1 (the subagent)", len(*requests))
		}
		sub := (*requests)[0]
		if len(sub.Messages) != 1 || sub.Messages[0].Role != "user" {
			t.Fatalf("subagent messages = %+v, want one user message", sub.Messages)
		}
		if prompt := sub.text(t, 0); prompt != apple.Msg("prod") {
			t.Errorf("subagent prompt = %q, want %q", prompt, apple.Msg("prod"))
		}
		if got := userTexts(t, msgs); !slices.Equal(got, []string{forked}) {
			t.Errorf("user messages = %q, want [%q]", got, forked)
		}
	})

	t.Run("stacked with an inline skill forks only the fork skill", func(t *testing.T) {
		client, requests := fakeModel(t, sequence(contentReply("blueberry")))

		msgs, err := chat.BuildMessages(t.Context(), client, "/grape /apple x", reg)
		checkErr(t, err, "")
		if len(*requests) != 1 {
			t.Fatalf("got %d requests, want 1 (the subagent)", len(*requests))
		}
		want := []string{grape.Msg("x"), forked}
		if got := userTexts(t, msgs); !slices.Equal(got, want) {
			t.Errorf("user messages = %q, want %q", got, want)
		}
	})

	t.Run("subagent failure is an error", func(t *testing.T) {
		client, _ := fakeModel(t, sequence(reply{status: http.StatusBadRequest, body: `{}`}))

		_, err := chat.BuildMessages(t.Context(), client, "/apple", reg)
		checkErr(t, err, "skill apple: ")
		checkErr(t, err, "400 Bad Request")
	})
}
