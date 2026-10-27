package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Parameter struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Function struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Parameters  Parameter `json:"parameters"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// ToParam converts the static Tool struct into the OpenAI SDK parameter type.
func (t Tool) ToParam() openai.ChatCompletionToolUnionParam {
	props := make(map[string]any, len(t.Function.Parameters.Properties))
	for k, v := range t.Function.Parameters.Properties {
		props[k] = map[string]any{
			"type":        v.Type,
			"description": v.Description,
		}
	}

	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        t.Function.Name,
		Description: openai.String(t.Function.Description),
		Parameters: shared.FunctionParameters{
			"type":       t.Function.Parameters.Type,
			"properties": props,
			"required":   t.Function.Parameters.Required,
		},
	})
}

var ReadTool = Tool{
	Type: "function",
	Function: Function{
		Name:        "Read",
		Description: "Read and return the contents of a file",
		Parameters: Parameter{
			Type: "object",
			Properties: map[string]Property{
				"file_path": {
					Type:        "string",
					Description: "The path to the file to read",
				},
			},
			Required: []string{"file_path"},
		},
	},
}

type ReadArgs struct {
	FilePath string `json:"file_path"`
}

func ReadFunc(_ context.Context, rawArgs string) ([]byte, error) {
	var args ReadArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}

	return os.ReadFile(args.FilePath)
}

var WriteTool = Tool{
	Type: "function",
	Function: Function{
		Name:        "Write",
		Description: "Write content to a file",
		Parameters: Parameter{
			Type: "object",
			Properties: map[string]Property{
				"file_path": {
					Type:        "string",
					Description: "The path of the file to write to",
				},
				"content": {
					Type:        "string",
					Description: "The content to write to the file",
				},
			},
			Required: []string{"file_path", "content"},
		},
	},
}

func WriteStringToFile(filepath, content string) error {
	return os.WriteFile(filepath, []byte(content), 0644)
}

type WriteArgs struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func WriteFunc(_ context.Context, rawArgs string) ([]byte, error) {
	var args WriteArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}

	return nil, WriteStringToFile(args.FilePath, args.Content)
}

var BashTool = Tool{
	Type: "function",
	Function: Function{
		Name:        "Bash",
		Description: "Execute a shell command",
		Parameters: Parameter{
			Type: "object",
			Properties: map[string]Property{
				"command": {
					Type:        "string",
					Description: "The command to execute",
				},
			},
			Required: []string{"command"},
		},
	},
}

type ProcessError struct {
	Code int
	Err  error
}

func (e *ProcessError) Error() string {
	return fmt.Sprintf("command exited with code %d: %v", e.Code, e.Err)
}

func (e *ProcessError) Unwrap() error {
	return e.Err
}

type commandResult struct {
	output []byte
	err    error
}

func runCommand(ctx context.Context, args []string, gracePeriod time.Duration) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("command arguments slice cannot be empty")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	// Send SIGTERM on context cancellation instead of the default SIGKILL.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}

	// Maximum time to wait after SIGTERM before escalating to SIGKILL.
	cmd.WaitDelay = gracePeriod

	resCh := make(chan commandResult, 1)

	go func() {
		out, err := cmd.CombinedOutput()
		resCh <- commandResult{
			output: out,
			err:    err,
		}
	}()

	// Blocks until the command and its I/O pipes have fully closed,
	// ensuring all buffers are drained even after cancellation.
	res := <-resCh

	if res.err != nil {
		code := -1

		if exitErr, ok := errors.AsType[*exec.ExitError](res.err); ok {
			code = exitErr.ExitCode()
		}

		return res.output, &ProcessError{
			Code: code,
			Err:  res.err,
		}
	}

	return res.output, nil
}

type BashArgs struct {
	Command string `json:"command"`
}

const BASH_DURATION = 100

func BashFunc(ctx context.Context, rawArgs string) ([]byte, error) {
	var args BashArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}

	return runCommand(ctx, strings.Fields(args.Command), time.Second*time.Duration(BASH_DURATION))
}

var ToolsRegistry = map[string]func(ctx context.Context, rawArgs string) ([]byte, error){
	"Read":  ReadFunc,
	"Write": WriteFunc,
	"Bash":  BashFunc,
}

func Execute(ctx context.Context, name string, rawArgs string) ([]byte, error) {
	fn, ok := ToolsRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return fn(ctx, rawArgs)
}

var ToolsList = []openai.ChatCompletionToolUnionParam{
	ReadTool.ToParam(),
	WriteTool.ToParam(),
	BashTool.ToParam(),
}
