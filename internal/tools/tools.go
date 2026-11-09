package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
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

type ToolFunc func(ctx context.Context, rawArgs string) (Result, error)

func withArgs[T any](fn func(context.Context, T) (Result, error)) ToolFunc {
	return func(ctx context.Context, rawArgs string) (Result, error) {
		var args T
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return Result{}, err
		}

		return fn(ctx, args)
	}
}

type readArgs struct {
	FilePath string `json:"file_path"`
}

func readFunc(_ context.Context, args readArgs) (Result, error) {
	b, err := os.ReadFile(args.FilePath)
	return Result{Output: string(b)}, err
}

type writeArgs struct {
	FilePath *string `json:"file_path"`
	Content  *string `json:"content"`
}

func writeFunc(_ context.Context, args writeArgs) (Result, error) {
	if args.Content == nil {
		return Result{}, errors.New(`missing required argument "content"`)
	}

	if args.FilePath == nil {
		return Result{}, errors.New(`missing required argument "file_path"`)
	}
	return Result{}, os.WriteFile(*args.FilePath, []byte(*args.Content), 0o600)
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

	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "OPENROUTER_API_KEY=")
	})

	// Send SIGTERM on context cancellation instead of the default SIGKILL.
	setProcessGroup(cmd)

	// Maximum time to wait after SIGTERM before escalating to SIGKILL.
	cmd.WaitDelay = gracePeriod

	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
		return string(out), nil
	}
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

type bashArgs struct {
	Command string `json:"command"`
}

const (
	bashGracePeriod = 5 * time.Second
	bashTimeout     = 2 * time.Minute
)

var errCommandTimeout = errors.New("command timed out")

func bashFunc(ctx context.Context, args bashArgs, timeout time.Duration) (Result, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, errCommandTimeout)
	defer cancel()

	cmdRunner := "sh"

	if _, err := exec.LookPath("bash"); err == nil {
		cmdRunner = "bash"
	}

	out, err := runCommand(ctx, []string{cmdRunner, "-c", args.Command}, bashGracePeriod)
	if errors.Is(context.Cause(ctx), errCommandTimeout) {
		return Result{Output: out}, fmt.Errorf("%w after %v", errCommandTimeout, timeout)
	}

	return Result{Output: out}, err
}

type skillArgs struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type Registry struct {
	Index map[string]ToolFunc
	List  []openai.ChatCompletionToolUnionParam
}

func NewToolsRegistry(s skills.SkillRegistry) Registry {
	return Registry{
		Index: map[string]ToolFunc{
			"Read":  withArgs(readFunc),
			"Write": withArgs(writeFunc),
			"Bash": withArgs(func(ctx context.Context, a bashArgs) (Result, error) {
				return bashFunc(ctx, a, bashTimeout)
			}),
			"Skill": withArgs(func(_ context.Context, args skillArgs) (Result, error) {
				skill, ok := s[args.Name]
				if !ok {
					return Result{}, fmt.Errorf("unknown skill: %s", args.Name)
				}

				return Result{
					Output: skill.Msg(args.Args),
					Fork:   skill.Fork(),
				}, nil
			}),
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
