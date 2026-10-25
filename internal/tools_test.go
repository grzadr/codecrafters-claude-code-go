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

func TestToolsList(t *testing.T) {
	if len(tools.ToolsList) != 1 {
		t.Fatalf("expected 1 tool in ToolsList, got %d", len(tools.ToolsList))
	}
	if tools.ToolsList[0].OfFunction.Function.Name != "Read" {
		t.Errorf("expected function name 'Read', got %q", tools.ToolsList[0].OfFunction.Function.Name)
	}
}

func TestToolsList(t *testing.T) {
	if len(tools.ToolsList) != 1 {
		t.Fatalf("expected 1 tool in ToolsList, got %d", len(tools.ToolsList))
	}
	if tools.ToolsList[0].OfFunction.Function.Name != "Read" {
		t.Errorf("expected function name 'Read', got %q", tools.ToolsList[0].OfFunction.Function.Name)
	}
}

