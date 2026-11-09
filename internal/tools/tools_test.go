package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	tools "github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

func TestRead(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	expectedContent := "Hello from Codecrafters Claude Code!"
	if err := os.WriteFile(filePath, []byte(expectedContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	reg := tools.NewToolsRegistry(nil)

	tests := []struct {
		name      string
		rawArgs   string
		want      string
		wantErr   bool
		targetErr error
	}{
		{
			name:    "successful read",
			rawArgs: `{"file_path":"` + filePath + `"}`,
			want:    expectedContent,
		},
		{
			name:    "invalid json",
			rawArgs: "{invalid-json",
			wantErr: true,
		},
		{
			name:      "file not found",
			rawArgs:   `{"file_path":"/nonexistent/file/path.txt"}`,
			wantErr:   true,
			targetErr: os.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := reg.Execute(t.Context(), "Read", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Read) error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.targetErr != nil && !errors.Is(err, tt.targetErr) {
				t.Errorf("expected error %v, got %v", tt.targetErr, err)
			}
			if res.Output != tt.want {
				t.Errorf("got %q, want %q", res.Output, tt.want)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "output.txt")
	content := "Hello from Write tool test!"

	reg := tools.NewToolsRegistry(nil)

	tests := []struct {
		name    string
		rawArgs string
		wantErr bool
	}{
		{
			name:    "successful write",
			rawArgs: `{"file_path":"` + filePath + `","content":"` + content + `"}`,
		},
		{
			name:    "invalid json",
			rawArgs: "{invalid-json",
			wantErr: true,
		},
		{
			name:    "failed write to nonexistent dir",
			rawArgs: `{"file_path":"/nonexistent/dir/file.txt","content":"data"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := reg.Execute(t.Context(), "Write", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Write) error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if res.Output != "" {
					t.Errorf("expected empty output, got %q", res.Output)
				}
				data, readErr := os.ReadFile(filePath)
				if readErr != nil {
					t.Fatalf("failed to read written file: %v", readErr)
				}
				if string(data) != content {
					t.Errorf("got file content %q, want %q", string(data), content)
				}
			}
		})
	}
}

func assertWriteError(t *testing.T, err error, wantErr string) {
	t.Helper()
	switch {
	case wantErr == "" && err != nil:
		t.Fatalf("unexpected error: %v", err)
	case wantErr != "" && (err == nil || err.Error() != wantErr):
		t.Fatalf("error = %v, want %q", err, wantErr)
	}
}

func TestWrite_ContentArgument(t *testing.T) {
	reg := tools.NewToolsRegistry(nil)

	tests := []struct {
		name     string
		args     string // %s is replaced by the target file path
		existing bool   // the target file starts with "old"
		wantErr  string
		wantFile string
	}{
		{
			name:     "missing content key is an error and keeps the file",
			args:     `{"file_path":"%s"}`,
			existing: true,
			wantErr:  `missing required argument "content"`,
			wantFile: "old",
		},
		{
			name:     "misnamed contents key is an error and keeps the file",
			args:     `{"file_path":"%s","contents":"new"}`,
			existing: true,
			wantErr:  `missing required argument "content"`,
			wantFile: "old",
		},
		{
			name:     "explicit empty content creates an empty file",
			args:     `{"file_path":"%s","content":""}`,
			wantFile: "",
		},
		{
			name:     "missing file_path key is an error and keeps the file",
			args:     `{"content":"new"}`,
			existing: true,
			wantErr:  `missing required argument "file_path"`,
			wantFile: "old",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.txt")
			if tt.existing {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			raw := tt.args
			if strings.Contains(raw, "%s") {
				raw = fmt.Sprintf(raw, path)
			}

			_, err := reg.Execute(t.Context(), "Write", raw)
			assertWriteError(t, err, tt.wantErr)

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}
			if string(data) != tt.wantFile {
				t.Errorf("file content = %q, want %q", data, tt.wantFile)
			}
		})
	}
}

func TestBash(t *testing.T) {
	reg := tools.NewToolsRegistry(nil)

	tests := []struct {
		name     string
		rawArgs  string
		want     string
		wantErr  bool
		wantCode int
	}{
		{
			name:    "successful command with pipes and quotes",
			rawArgs: `{"command":"echo 'hello world' | tr a-z A-Z"}`,
			want:    "HELLO WORLD",
		},
		{
			name:    "invalid json",
			rawArgs: "{invalid-json",
			wantErr: true,
		},
		{
			name:     "command exit error",
			rawArgs:  `{"command":"false"}`,
			wantErr:  true,
			wantCode: 1,
		},
		{
			name:     "command not found in shell",
			rawArgs:  `{"command":"nonexistent-tool-binary-xyz"}`,
			wantErr:  true,
			wantCode: 127,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := reg.Execute(t.Context(), "Bash", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Bash) error = %v, wantErr %v", err, tt.wantErr)
			}
			procErr, ok := errors.AsType[*tools.ProcessError](err)
			if ok && procErr.Code != tt.wantCode {
				t.Errorf("got exit code %d, want %d", procErr.Code, tt.wantCode)
			}
			if got := strings.TrimSpace(res.Output); !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(20*time.Millisecond, cancel)

		_, err := reg.Execute(ctx, "Bash", `{"command":"sleep 2"}`)
		if err == nil {
			t.Fatal("expected error on canceled context, got nil")
		}
	})
}

func TestBash_Environment(t *testing.T) {
	t.Run("runs bash", func(t *testing.T) {
		// Bashisms only work in bash, and $BASH names the binary that runs the command.
		res, err := tools.NewToolsRegistry(nil).Execute(
			t.Context(),
			"Bash",
			`{"command":"[[ -d / ]] && echo {1..2} ${BASH##*/}"}`,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v (output %q)", err, res.Output)
		}
		if res.Output != "1 2 bash\n" {
			t.Errorf("got %q, want %q", res.Output, "1 2 bash\n")
		}
	})

	t.Run("falls back to sh", func(t *testing.T) {
		sh, err := exec.LookPath("sh")
		if err != nil {
			t.Skipf("sh not on PATH: %v", err)
		}
		dir := t.TempDir()
		if err = os.Symlink(sh, filepath.Join(dir, "sh")); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)

		res, err := tools.NewToolsRegistry(nil).
			Execute(t.Context(), "Bash", `{"command":"echo ok"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v (output %q)", err, res.Output)
		}
		if res.Output != "ok\n" {
			t.Errorf("got %q, want %q", res.Output, "ok\n")
		}
	})

	t.Run("api key is not passed to the command", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "test-key")

		res, err := tools.NewToolsRegistry(nil).Execute(
			t.Context(),
			"Bash",
			`{"command":"printenv OPENROUTER_API_KEY; echo done"}`,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v (output %q)", err, res.Output)
		}
		if res.Output != "done\n" {
			t.Errorf("got %q, want %q (the key leaked into the child)", res.Output, "done\n")
		}
	})
}

func TestBash_Timeout(t *testing.T) {
	t.Run("command timeout keeps the output so far", func(t *testing.T) {
		res, err := tools.BashFunc(t.Context(), "echo started; sleep 5", 100*time.Millisecond)
		if err == nil || err.Error() != "command timed out after 100ms" {
			t.Fatalf("error = %v, want %q", err, "command timed out after 100ms")
		}
		if res.Output != "started\n" {
			t.Errorf("output = %q, want %q", res.Output, "started\n")
		}
	})

	t.Run("parent deadline is not a command timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		_, err := tools.BashFunc(ctx, "sleep 5", time.Minute)
		if err == nil {
			t.Fatal("expected an error after the parent deadline, got nil")
		}
		if strings.Contains(err.Error(), "command timed out") {
			t.Errorf("error = %v, want a non-timeout error", err)
		}
	})
}

func TestSkill(t *testing.T) {
	apple := skills.Skill{
		Name:        "apple",
		Description: "Use this skill when the user asks for the database migration status.",
		Path:        ".claude/skills/apple",
		Body:        "\nRespond with exactly one word: blueberry\n",
	}
	reg := tools.NewToolsRegistry(skills.SkillRegistry{"apple": apple})

	tests := []struct {
		name    string
		rawArgs string
		want    string
		wantErr bool
	}{
		{
			name:    "matching skill",
			rawArgs: `{"name":"apple"}`,
			want:    apple.Msg(""),
		},
		{
			name:    "matching skill with optional args",
			rawArgs: `{"name":"apple","args":"prod"}`,
			want:    apple.Msg("prod"),
		},
		{
			name:    "args are passed verbatim",
			rawArgs: `{"name":"apple","args":"a\n  b"}`,
			want: "Skill: apple (located at .claude/skills/apple)\n" +
				"Paths in the instructions below are relative to that folder.\n\n" +
				"Respond with exactly one word: blueberry\n\nARGUMENTS: a\n  b",
		},
		{
			name:    "unknown skill",
			rawArgs: `{"name":"pear"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			rawArgs: "{invalid-json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := reg.Execute(t.Context(), "Skill", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Skill) error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (res.Output != tt.want || res.Fork) {
				t.Errorf("got (%q, fork=%v), want (%q, fork=false)", res.Output, res.Fork, tt.want)
			}
		})
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	reg := tools.NewToolsRegistry(nil)
	_, err := reg.Execute(t.Context(), "NonExistentTool", "{}")
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected error message to mention unknown tool, got: %v", err)
	}
}

func TestToolsRegistry_RegisterCustomTool(t *testing.T) {
	type GreetArgs struct {
		Name string `json:"name"`
	}

	reg := tools.NewToolsRegistry(nil)
	greetCalled := false
	reg.Index["Greet"] = func(ctx context.Context, rawArgs string) (tools.Result, error) {
		if ctx == nil {
			return tools.Result{}, errors.New("expected non-nil context")
		}
		var args GreetArgs
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return tools.Result{}, err
		}
		if args.Name != "Claude" {
			return tools.Result{}, errors.New("unexpected name")
		}
		greetCalled = true
		return tools.Result{Output: "Hello, Claude!"}, nil
	}

	res, err := reg.Execute(t.Context(), "Greet", `{"name":"Claude"}`)
	if err != nil {
		t.Fatalf("unexpected error for custom tool: %v", err)
	}
	if !greetCalled {
		t.Fatal("expected custom tool to be called")
	}
	if res.Output != "Hello, Claude!" {
		t.Errorf("expected %q, got %q", "Hello, Claude!", res.Output)
	}
}

func TestNewToolsRegistry(t *testing.T) {
	reg := tools.NewToolsRegistry(nil)
	wantNames := []string{"Read", "Write", "Bash", "Skill"}

	t.Run("params advertise every tool in order", func(t *testing.T) {
		params := reg.Params()
		if len(params) != len(wantNames) {
			t.Fatalf("got %d params, want %d", len(params), len(wantNames))
		}
		for i, want := range wantNames {
			if got := params[i].OfFunction.Function.Name; got != want {
				t.Errorf("params[%d] name = %q, want %q", i, got, want)
			}
		}
	})

	t.Run("every listed tool is executable", func(t *testing.T) {
		for _, name := range wantNames {
			if _, ok := reg.Get(name); !ok {
				t.Errorf("Get(%q) not found", name)
			}
		}
		if _, ok := reg.Get("Missing"); ok {
			t.Error("Get(\"Missing\") found, want not found")
		}
	})

	t.Run("skill parameters", func(t *testing.T) {
		fn := reg.Params()[3].OfFunction.Function
		got, err := json.Marshal(fn.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"properties":{"args":{"description":"Optional arguments for the skill","type":"string"},` +
			`"name":{"description":"The name of the skill to use","type":"string"}},` +
			`"required":["name"],"type":"object"}`
		if string(got) != want {
			t.Errorf("Skill parameters = %s, want %s", got, want)
		}
	})
}

func TestSkill_Fork(t *testing.T) {
	apple := skills.Skill{
		Name:    "apple",
		Context: "fork",
		Path:    ".claude/skills/apple",
		Body:    "Respond with exactly one word: blueberry\n",
	}
	reg := tools.NewToolsRegistry(skills.SkillRegistry{"apple": apple})

	res, err := reg.Execute(t.Context(), "Skill", `{"name":"apple"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Fork {
		t.Error("Fork = false, want true")
	}
	if res.Output != apple.Msg("") {
		t.Errorf("got %q, want %q", res.Output, apple.Msg(""))
	}
}

func TestProcessError(t *testing.T) {
	baseErr := errors.New("underlying command error")
	procErr := &tools.ProcessError{
		Code: 42,
		Err:  baseErr,
	}

	expectedMsg := "command exited with code 42: underlying command error"
	if procErr.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, procErr.Error())
	}

	if !errors.Is(procErr, baseErr) {
		t.Errorf("expected errors.Is to match baseErr")
	}

	if !errors.Is(procErr.Unwrap(), baseErr) {
		t.Errorf("expected Unwrap to return baseErr, got %v", procErr.Unwrap())
	}
}

func TestRunCommand(t *testing.T) {
	t.Run("empty args", func(t *testing.T) {
		_, err := tools.RunCommand(t.Context(), nil, time.Second)
		if err == nil {
			t.Fatal("expected error for empty args, got nil")
		}
		if err.Error() != "command arguments slice cannot be empty" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("binary not found returns code -1", func(t *testing.T) {
		_, err := tools.RunCommand(t.Context(), []string{"nonexistent-binary-xyz"}, time.Second)
		procErr, ok := errors.AsType[*tools.ProcessError](err)
		if !ok {
			t.Fatalf("expected *tools.ProcessError, got %T: %v", err, err)
		}
		if procErr.Code != -1 {
			t.Errorf("expected exit code -1, got %d", procErr.Code)
		}
	})

	t.Run("cancel stops the shell's children", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(100*time.Millisecond, cancel)

		start := time.Now()
		_, err := tools.RunCommand(ctx, []string{"sh", "-c", "sleep 30; true"}, 30*time.Second)
		if err == nil {
			t.Fatal("expected error on canceled context, got nil")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("returned after %v, want under 5s", elapsed)
		}
	})

	t.Run("background child does not fail a successful command", func(t *testing.T) {
		start := time.Now()
		out, err := tools.RunCommand(
			t.Context(),
			[]string{"sh", "-c", "sleep 5 & echo started"},
			time.Second,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "started\n" {
			t.Errorf("got %q, want %q", out, "started\n")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("returned after %v, want under 2s", elapsed)
		}
	})
}
