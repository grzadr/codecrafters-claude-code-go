package tools_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "github.com/codecrafters-io/claude-code-starter-go/internal"
)

func TestExecute_Read(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	expectedContent := "Hello from Codecrafters Claude Code!"
	if err := os.WriteFile(filePath, []byte(expectedContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	rawArgs := `{"file_path":"` + filePath + `"}`
	output, err := tools.Execute("Read", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error executing Read: %v", err)
	}
	if string(output) != expectedContent {
		t.Errorf("expected output %q, got %q", expectedContent, string(output))
	}
}

func TestExecute_Read_InvalidJSON(t *testing.T) {
	_, err := tools.Execute("Read", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_Read_FileNotFound(t *testing.T) {
	rawArgs := `{"file_path":"/nonexistent/file/path.txt"}`
	_, err := tools.Execute("Read", rawArgs)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	_, err := tools.Execute("NonExistentTool", "{}")
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
	tools.ToolsRegistry["Greet"] = func(rawArgs string) ([]byte, error) {
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

	res, err := tools.Execute("Greet", `{"name":"Claude"}`)
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
	output, err := tools.Execute("Write", rawArgs)
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
	_, err := tools.Execute("Write", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_Write_FailedWrite(t *testing.T) {
	rawArgs := `{"file_path":"/nonexistent/dir/file.txt","content":"data"}`
	_, err := tools.Execute("Write", rawArgs)
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
	output, err := tools.Execute("Bash", rawArgs)
	if err != nil {
		t.Fatalf("unexpected error executing Bash: %v", err)
	}
	if len(output) != 0 {
		t.Errorf("expected empty output from dummy runCommand, got %q", string(output))
	}
}

func TestExecute_Bash_InvalidJSON(t *testing.T) {
	_, err := tools.Execute("Bash", "{invalid-json")
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
