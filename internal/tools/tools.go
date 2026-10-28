package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
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

const (
	toolTypeFunction = "function"
	paramTypeObject  = "object"
	propTypeString   = "string"
	propFilePath     = "file_path"
)

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

func WriteStringToFile(filePath, content string) error {
	return os.WriteFile(filePath, []byte(content), 0o600)
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

func runCommand(ctx context.Context, args []string, gracePeriod time.Duration) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("command arguments slice cannot be empty")
	}

	//nolint:gosec // G204: Bash tool intentionally executes user/LLM-provided commands
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	// Send SIGTERM on context cancellation instead of the default SIGKILL.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}

	// Maximum time to wait after SIGTERM before escalating to SIGKILL.
	cmd.WaitDelay = gracePeriod

	out, err := cmd.CombinedOutput()
	if err != nil {
		code := -1

		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exitErr.ExitCode()
		}

		return out, &ProcessError{
			Code: code,
			Err:  err,
		}
	}

	return out, nil
}

type BashArgs struct {
	Command string `json:"command"`
}

const bashGracePeriod = 100 * time.Second

func BashFunc(ctx context.Context, rawArgs string) ([]byte, error) {
	var args BashArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}

	return runCommand(ctx, []string{"sh", "-c", args.Command}, bashGracePeriod)
}

type Registry map[string]func(ctx context.Context, rawArgs string) ([]byte, error)

func newToolsRegistry(s skills.SkillRegistry) Registry {
	return Registry{
		"Read":  ReadFunc,
		"Write": WriteFunc,
		"Bash":  BashFunc,
		"Skill": func(_ context.Context, rawArgs string) ([]byte, error) {
			var args struct {
				Name string `json:"name"`
				Args string `json:"args"`
			}
			if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
				return nil, err
			}

			s := s[args.Name]

			return []byte(s.Msg()), nil
		},
	}
}

func (r Registry) Execute(ctx context.Context, name, rawArgs string) ([]byte, error) {
	fn, ok := r[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return fn(ctx, rawArgs)
}

func newReadTool() Tool {
	return Tool{
		Type: toolTypeFunction,
		Function: Function{
			Name:        "Read",
			Description: "Read and return the contents of a file",
			Parameters: Parameter{
				Type: paramTypeObject,
				Properties: map[string]Property{
					propFilePath: {
						Type:        propTypeString,
						Description: "The path to the file to read",
					},
				},
				Required: []string{propFilePath},
			},
		},
	}
}

func newWriteTool() Tool {
	return Tool{
		Type: toolTypeFunction,
		Function: Function{
			Name:        "Write",
			Description: "Write content to a file",
			Parameters: Parameter{
				Type: paramTypeObject,
				Properties: map[string]Property{
					propFilePath: {
						Type:        propTypeString,
						Description: "The path of the file to write to",
					},
					"content": {
						Type:        propTypeString,
						Description: "The content to write to the file",
					},
				},
				Required: []string{propFilePath, "content"},
			},
		},
	}
}

func newBashTool() Tool {
	return Tool{
		Type: toolTypeFunction,
		Function: Function{
			Name:        "Bash",
			Description: "Execute a shell command",
			Parameters: Parameter{
				Type: paramTypeObject,
				Properties: map[string]Property{
					"command": {
						Type:        propTypeString,
						Description: "The command to execute",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}

func newSkillTool() Tool {
	return Tool{
		Type: toolTypeFunction,
		Function: Function{
			Name:        "Skill",
			Description: "Load a skill's instructions into the conversation",
			Parameters: Parameter{
				Type: paramTypeObject,
				Properties: map[string]Property{
					"name": {
						Type:        propTypeString,
						Description: "The name of the skill to use",
					},
					"args": {
						Type:        propTypeString,
						Description: "Optional arguments for the skill",
					},
				},
				Required: []string{"name"},
			},
		},
	}
}

func InitTools(
	s skills.SkillRegistry,
) (Registry, []openai.ChatCompletionToolUnionParam) {
	return newToolsRegistry(s), []openai.ChatCompletionToolUnionParam{
		newReadTool().ToParam(),
		newWriteTool().ToParam(),
		newBashTool().ToParam(),
		newSkillTool().ToParam(),
	}
}
