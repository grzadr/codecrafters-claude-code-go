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

func newTool(
	name, desc string,
	props map[string]string,
	required ...string,
) openai.ChatCompletionToolUnionParam {
	properties := make(map[string]any, len(props))
	for k, d := range props {
		properties[k] = map[string]any{
			"type":        "string",
			"description": d,
		}
	}

	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        name,
		Description: openai.String(desc),
		Parameters: shared.FunctionParameters{
			"type":       "object",
			"properties": properties,
			"required":   required,
		},
	})
}

type Result struct {
	Output string
	Fork   bool
}

type ReadArgs struct {
	FilePath string `json:"file_path"`
}

func ReadFunc(_ context.Context, rawArgs string) (Result, error) {
	var args ReadArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return Result{}, err
	}

	b, err := os.ReadFile(args.FilePath)
	return Result{Output: string(b)}, err
}

type WriteArgs struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func WriteFunc(_ context.Context, rawArgs string) (Result, error) {
	var args WriteArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return Result{}, err
	}

	return Result{}, os.WriteFile(args.FilePath, []byte(args.Content), 0o600)
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

func runCommand(ctx context.Context, args []string, gracePeriod time.Duration) (string, error) {
	if len(args) == 0 {
		return "", errors.New("command arguments slice cannot be empty")
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

		return string(out), &ProcessError{
			Code: code,
			Err:  err,
		}
	}

	return string(out), nil
}

type BashArgs struct {
	Command string `json:"command"`
}

const bashGracePeriod = 100 * time.Second

func BashFunc(ctx context.Context, rawArgs string) (Result, error) {
	var args BashArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return Result{}, err
	}

	out, err := runCommand(ctx, []string{"sh", "-c", args.Command}, bashGracePeriod)
	return Result{Output: out}, err
}

type ToolFunc func(ctx context.Context, rawArgs string) (Result, error)

type Registry struct {
	Index map[string]ToolFunc
	List  []openai.ChatCompletionToolUnionParam
}

func NewToolsRegistry(s skills.SkillRegistry) Registry {
	return Registry{
		Index: map[string]ToolFunc{
			"Read":  ReadFunc,
			"Write": WriteFunc,
			"Bash":  BashFunc,
			"Skill": func(_ context.Context, rawArgs string) (Result, error) {
				var args struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
					return Result{}, err
				}

				skill, ok := s[args.Name]
				if !ok {
					return Result{}, fmt.Errorf("unknown skill: %s", args.Name)
				}

				return Result{Output: skill.Msg(), Fork: skill.Fork()}, nil
			},
		},
		List: []openai.ChatCompletionToolUnionParam{
			newTool("Read", "Read and return the contents of a file", map[string]string{
				"file_path": "The path to the file to read",
			}, "file_path"),
			newTool("Write", "Write content to a file", map[string]string{
				"file_path": "The path of the file to write to",
				"content":   "The content to write to the file",
			}, "file_path", "content"),
			newTool("Bash", "Execute a shell command", map[string]string{
				"command": "The command to execute",
			}, "command"),
			newTool("Skill", "Load a skill's instructions into the conversation", map[string]string{
				"name": "The name of the skill to use",
				"args": "Optional arguments for the skill",
			}, "name"),
		},
	}
}

func (r Registry) Get(name string) (ToolFunc, bool) {
	fn, ok := r.Index[name]

	return fn, ok
}

func (r Registry) Execute(ctx context.Context, name, rawArgs string) (Result, error) {
	fn, ok := r.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool: %s", name)
	}

	return fn(ctx, rawArgs)
}

func (r Registry) Params() []openai.ChatCompletionToolUnionParam {
	return r.List
}
