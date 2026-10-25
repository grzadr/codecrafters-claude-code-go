package tools_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "github.com/codecrafters-io/claude-code-starter-go/internal"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	origStdout := os.Stdout
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	fn()

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("failed to close writer: %v", closeErr)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read pipe output: %v", err)
	}
	return string(out)
}

func TestExecute_Read(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	expectedContent := "Hello from Codecrafters Claude Code!"
	if err := os.WriteFile(filePath, []byte(expectedContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	rawArgs := `{"file_path":"` + filePath + `"}`
	var execErr error
	output := captureStdout(t, func() {
		execErr = tools.Execute("Read", rawArgs)
	})

	if execErr != nil {
		t.Fatalf("unexpected error executing Read: %v", execErr)
	}
	if output != expectedContent {
		t.Errorf("expected stdout %q, got %q", expectedContent, output)
	}
}

func TestExecute_Read_InvalidJSON(t *testing.T) {
	err := tools.Execute("Read", "{invalid-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecute_Read_FileNotFound(t *testing.T) {
	rawArgs := `{"file_path":"/nonexistent/file/path.txt"}`
	err := tools.Execute("Read", rawArgs)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	err := tools.Execute("NonExistentTool", "{}")
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
	tools.ToolsRegistry["Greet"] = func(rawArgs string) error {
		var args GreetArgs
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return err
		}
		if args.Name != "Claude" {
			return errors.New("unexpected name")
		}
		greetCalled = true
		return nil
	}
	defer delete(tools.ToolsRegistry, "Greet")

	if err := tools.Execute("Greet", `{"name":"Claude"}`); err != nil {
		t.Fatalf("unexpected error for custom tool: %v", err)
	}
	if !greetCalled {
		t.Fatal("expected custom tool to be called")
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
