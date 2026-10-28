package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
