package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tools "github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

func TestExecute_Read(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	expectedContent := "Hello from Codecrafters Claude Code!"
	if err := os.WriteFile(filePath, []byte(expectedContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	rawArgs := `{"file_path":"` + filePath + `"}`
	output, err := tools.Execute(t.Context(), "Read", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error executing Read: %v", err)
	}
	if string(output) != expectedContent {
		t.Errorf("expected output %q, got %q", expectedContent, string(output))
	}
}

func TestExecute_Read_InvalidJSON(t *testing.T) {
	_, err := tools.Execute(t.Context(), "Read", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_Read_FileNotFound(t *testing.T) {
	rawArgs := `{"file_path":"/nonexistent/file/path.txt"}`
	_, err := tools.Execute(t.Context(), "Read", rawArgs)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	_, err := tools.Execute(t.Context(), "NonExistentTool", "{}")
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

	greetCalled := false
	tools.ToolsRegistry["Greet"] = func(ctx context.Context, rawArgs string) ([]byte, error) {
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
	defer delete(tools.ToolsRegistry, "Greet")

	res, err := tools.Execute(t.Context(), "Greet", `{"name":"Claude"}`)
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

func TestExecute_Write(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "output.txt")
	content := "Hello from Write tool test!"

	rawArgs := `{"file_path":"` + filePath + `","content":"` + content + `"}`
	output, err := tools.Execute(t.Context(), "Write", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error executing Write: %v", err)
	}
	if len(output) != 0 {
		t.Errorf("expected empty output, got %q", string(output))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected file content %q, got %q", content, string(data))
	}
}

func TestExecute_Write_InvalidJSON(t *testing.T) {
	_, err := tools.Execute(t.Context(), "Write", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_Write_FailedWrite(t *testing.T) {
	rawArgs := `{"file_path":"/nonexistent/dir/file.txt","content":"data"}`
	_, err := tools.Execute(t.Context(), "Write", rawArgs)
	if err == nil {
		t.Fatal("expected error when writing to invalid path, got nil")
	}
}

func TestWriteStringToFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "direct.txt")
	content := "Direct write test"

	if err := tools.WriteStringToFile(filePath, content); err != nil {
		t.Fatalf("unexpected error from WriteStringToFile: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestTool_ToParam(t *testing.T) {
	param := tools.ReadTool.ToParam()
	fnDef := param.OfFunction.Function

	if fnDef.Name != "Read" {
		t.Errorf("expected function name 'Read', got %q", fnDef.Name)
	}

	if fnDef.Description.Value != "Read and return the contents of a file" {
		t.Errorf("unexpected description: %v", fnDef.Description.Value)
	}

	params := fnDef.Parameters
	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}

	filePathProp, ok := props["file_path"].(map[string]any)
	if !ok {
		t.Fatalf("expected file_path property, got %T", props["file_path"])
	}

	if filePathProp["type"] != "string" {
		t.Errorf("expected string type for file_path, got %v", filePathProp["type"])
	}
}

func TestWriteTool_ToParam(t *testing.T) {
	param := tools.WriteTool.ToParam()
	fnDef := param.OfFunction.Function

	if fnDef.Name != "Write" {
		t.Errorf("expected function name 'Write', got %q", fnDef.Name)
	}

	if fnDef.Description.Value != "Write content to a file" {
		t.Errorf("unexpected description: %v", fnDef.Description.Value)
	}

	params := fnDef.Parameters
	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}

	filePathProp, ok := props["file_path"].(map[string]any)
	if !ok {
		t.Fatalf("expected file_path property, got %T", props["file_path"])
	}
	if filePathProp["type"] != "string" {
		t.Errorf("expected string type for file_path, got %v", filePathProp["type"])
	}
	if filePathProp["description"] != "The path of the file to write to" {
		t.Errorf("unexpected description for file_path: %v", filePathProp["description"])
	}

	contentProp, ok := props["content"].(map[string]any)
	if !ok {
		t.Fatalf("expected content property, got %T", props["content"])
	}
	if contentProp["type"] != "string" {
		t.Errorf("expected string type for content, got %v", contentProp["type"])
	}
	if contentProp["description"] != "The content to write to the file" {
		t.Errorf("unexpected description for content: %v", contentProp["description"])
	}
}

func TestExecute_Bash(t *testing.T) {
	rawArgs := `{"command":"echo hello"}`
	output, err := tools.Execute(t.Context(), "Bash", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error executing Bash: %v", err)
	}
	if strings.TrimSpace(string(output)) != "hello" {
		t.Errorf("expected output %q, got %q", "hello", strings.TrimSpace(string(output)))
	}
}

func TestExecute_Bash_InvalidJSON(t *testing.T) {
	_, err := tools.Execute(t.Context(), "Bash", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestBashTool_ToParam(t *testing.T) {
	param := tools.BashTool.ToParam()
	fnDef := param.OfFunction.Function

	if fnDef.Name != "Bash" {
		t.Errorf("expected function name 'Bash', got %q", fnDef.Name)
	}

	if fnDef.Description.Value != "Execute a shell command" {
		t.Errorf("unexpected description: %v", fnDef.Description.Value)
	}

	params := fnDef.Parameters
	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}

	commandProp, ok := props["command"].(map[string]any)
	if !ok {
		t.Fatalf("expected command property, got %T", props["command"])
	}
	if commandProp["type"] != "string" {
		t.Errorf("expected string type for command, got %v", commandProp["type"])
	}
	if commandProp["description"] != "The command to execute" {
		t.Errorf("unexpected description for command: %v", commandProp["description"])
	}
}

func TestToolsList(t *testing.T) {
	if len(tools.ToolsList) != 3 {
		t.Fatalf("expected 3 tools in ToolsList, got %d", len(tools.ToolsList))
	}
	if tools.ToolsList[0].OfFunction.Function.Name != "Read" {
		t.Errorf("expected first tool name 'Read', got %q", tools.ToolsList[0].OfFunction.Function.Name)
	}
	if tools.ToolsList[1].OfFunction.Function.Name != "Write" {
		t.Errorf("expected second tool name 'Write', got %q", tools.ToolsList[1].OfFunction.Function.Name)
	}
	if tools.ToolsList[2].OfFunction.Function.Name != "Bash" {
		t.Errorf("expected third tool name 'Bash', got %q", tools.ToolsList[2].OfFunction.Function.Name)
	}
}

func TestReadFunc(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "read_func_test.txt")
	expectedContent := "direct read func test"
	if err := os.WriteFile(filePath, []byte(expectedContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	t.Run("successful read", func(t *testing.T) {
		out, err := tools.ReadFunc(t.Context(), `{"file_path":"`+filePath+`"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(out) != expectedContent {
			t.Errorf("expected %q, got %q", expectedContent, string(out))
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := tools.ReadFunc(t.Context(), "{invalid")
		if err == nil {
			t.Fatal("expected error for invalid json, got nil")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := tools.ReadFunc(t.Context(), `{"file_path":"/nonexistent/read/path.txt"}`)
		if err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected os.ErrNotExist, got %v", err)
		}
	})
}

func TestWriteFunc(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "write_func_test.txt")
	content := "direct write func test"

	t.Run("successful write", func(t *testing.T) {
		out, err := tools.WriteFunc(t.Context(), `{"file_path":"`+filePath+`","content":"`+content+`"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Errorf("expected empty output, got %q", string(out))
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read written file: %v", err)
		}
		if string(data) != content {
			t.Errorf("expected %q, got %q", content, string(data))
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := tools.WriteFunc(t.Context(), "{invalid")
		if err == nil {
			t.Fatal("expected error for invalid json, got nil")
		}
	})

	t.Run("failed write", func(t *testing.T) {
		_, err := tools.WriteFunc(t.Context(), `{"file_path":"/nonexistent/dir/file.txt","content":"data"}`)
		if err == nil {
			t.Fatal("expected error for invalid path, got nil")
		}
	})
}

func TestBashFunc(t *testing.T) {
	t.Run("successful command", func(t *testing.T) {
		out, err := tools.BashFunc(t.Context(), `{"command":"echo hello world"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(string(out)) != "hello world" {
			t.Errorf("expected 'hello world', got %q", strings.TrimSpace(string(out)))
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := tools.BashFunc(t.Context(), "{invalid")
		if err == nil {
			t.Fatal("expected error for invalid json, got nil")
		}
	})

	t.Run("command exit error", func(t *testing.T) {
		_, err := tools.BashFunc(t.Context(), `{"command":"false"}`)
		if err == nil {
			t.Fatal("expected error for command exiting with failure, got nil")
		}

		procErr, ok := errors.AsType[*tools.ProcessError](err)
		if !ok {
			t.Fatalf("expected error of type *tools.ProcessError, got %T: %v", err, err)
		}
		if procErr.Code != 1 {
			t.Errorf("expected exit code 1, got %d", procErr.Code)
		}
	})

	t.Run("command not found", func(t *testing.T) {
		_, err := tools.BashFunc(t.Context(), `{"command":"nonexistent-tool-binary-xyz"}`)
		if err == nil {
			t.Fatal("expected error for non-existent command, got nil")
		}

		procErr, ok := errors.AsType[*tools.ProcessError](err)
		if !ok {
			t.Fatalf("expected error of type *tools.ProcessError, got %T: %v", err, err)
		}
		if procErr.Code != -1 {
			t.Errorf("expected exit code -1, got %d", procErr.Code)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(20*time.Millisecond, cancel)

		_, err := tools.BashFunc(ctx, `{"command":"sleep 2"}`)
		if err == nil {
			t.Fatal("expected error on canceled context, got nil")
		}
	})
}

func TestBashArgs_Split(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected []string
	}{
		{
			name:     "single word",
			command:  "ls",
			expected: []string{"ls"},
		},
		{
			name:     "command with arguments",
			command:  "echo hello world",
			expected: []string{"echo", "hello", "world"},
		},
		{
			name:     "empty command",
			command:  "",
			expected: []string{""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := tools.BashArgs{Command: tc.command}
			got := args.Split()
			if !slices.Equal(got, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
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

	if procErr.Unwrap() != baseErr {
		t.Errorf("expected Unwrap to return baseErr, got %v", procErr.Unwrap())
	}
}

func TestRunCommand_EmptyArgs(t *testing.T) {
	_, err := tools.RunCommand(t.Context(), nil, time.Second)
	if err == nil {
		t.Fatal("expected error for empty args, got nil")
	}
	if err.Error() != "command arguments slice cannot be empty" {
		t.Errorf("unexpected error message: %v", err)
	}
}
