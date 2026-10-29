package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

	reg, _ := tools.InitTools(nil)

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
			out, err := reg.Execute(t.Context(), "Read", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Read) error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.targetErr != nil && !errors.Is(err, tt.targetErr) {
				t.Errorf("expected error %v, got %v", tt.targetErr, err)
			}
			if string(out) != tt.want {
				t.Errorf("got %q, want %q", string(out), tt.want)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "output.txt")
	content := "Hello from Write tool test!"

	reg, _ := tools.InitTools(nil)

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
			out, err := reg.Execute(t.Context(), "Write", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Write) error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(out) != 0 {
					t.Errorf("expected empty output, got %q", string(out))
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

func TestBash(t *testing.T) {
	reg, _ := tools.InitTools(nil)

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
			out, err := reg.Execute(t.Context(), "Bash", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Bash) error = %v, wantErr %v", err, tt.wantErr)
			}
			procErr, ok := errors.AsType[*tools.ProcessError](err)
			if ok && procErr.Code != tt.wantCode {
				t.Errorf("got exit code %d, want %d", procErr.Code, tt.wantCode)
			}
			if got := strings.TrimSpace(string(out)); !tt.wantErr && got != tt.want {
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

func TestSkill(t *testing.T) {
	apple := skills.Skill{
		Name:        "apple",
		Description: "Use this skill when the user asks for the database migration status.",
		Path:        ".claude/skills/apple",
		Body:        []byte("\nRespond with exactly one word: blueberry\n"),
	}
	reg, _ := tools.InitTools(skills.SkillRegistry{"apple": apple})

	tests := []struct {
		name    string
		rawArgs string
		want    string
		wantErr bool
	}{
		{
			name:    "matching skill",
			rawArgs: `{"name":"apple"}`,
			want:    apple.Msg(),
		},
		{
			name:    "matching skill with optional args",
			rawArgs: `{"name":"apple","args":"prod"}`,
			want:    apple.Msg(),
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
			out, err := reg.Execute(t.Context(), "Skill", tt.rawArgs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute(Skill) error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(out) != tt.want {
				t.Errorf("got %q, want %q", string(out), tt.want)
			}
		})
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	reg, _ := tools.InitTools(nil)
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

	reg, _ := tools.InitTools(nil)
	greetCalled := false
	reg["Greet"] = func(ctx context.Context, rawArgs string) ([]byte, error) {
		if ctx == nil {
			return nil, errors.New("expected non-nil context")
		}
		var args GreetArgs
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return nil, err
		}
		if args.Name != "Claude" {
			return nil, errors.New("unexpected name")
		}
		greetCalled = true
		return []byte("Hello, Claude!"), nil
	}

	res, err := reg.Execute(t.Context(), "Greet", `{"name":"Claude"}`)
	if err != nil {
		t.Fatalf("unexpected error for custom tool: %v", err)
	}
	if !greetCalled {
		t.Fatal("expected custom tool to be called")
	}
	if string(res) != "Hello, Claude!" {
		t.Errorf("expected %q, got %q", "Hello, Claude!", string(res))
	}
}

func TestInitTools(t *testing.T) {
	_, toolsList := tools.InitTools(nil)
	wantNames := []string{"Read", "Write", "Bash", "Skill"}
	if len(toolsList) != len(wantNames) {
		t.Fatalf("got %d tools in toolsList, want %d", len(toolsList), len(wantNames))
	}
	for i, want := range wantNames {
		if got := toolsList[i].OfFunction.Function.Name; got != want {
			t.Errorf("toolsList[%d] name = %q, want %q", i, got, want)
		}
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
}
