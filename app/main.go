package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	"github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

//nolint:gochecknoglobals // stubbed in tests
var exit = os.Exit

var errRunTimeout = errors.New("run timed out")

const exitCodeInterrupted = 130

func newMainArgs(args []string) (string, time.Duration, error) {
	var prompt string
	var timeout time.Duration

	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&prompt, "p", "", "Prompt to send to LLM")
	fs.DurationVar(&timeout, "timeout", 0, "Abort the run after this long (0 = no limit)")

	if err := fs.Parse(args); err != nil {
		return "", 0, err
	}

	if fs.NArg() > 0 {
		return "", 0, fmt.Errorf("unexpected arguments %q: quote the prompt", fs.Args())
	}

	if prompt == "" {
		return "", 0, errors.New(`prompt must not be empty: use -p "<prompt>"`)
	}

	return prompt, timeout, nil
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	prompt, d, err := newMainArgs(args)
	if err != nil {
		return err
	}

	if d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, d, errRunTimeout)
		defer cancel()
	}

	client, err := chat.NewOpenAiClient()
	if err != nil {
		return err
	}

	skillsRegistry := skills.InitSkillsRegistry(".claude/skills")
	toolsRegistry := tools.NewToolsRegistry(skillsRegistry)

	messages, err := chat.BuildMessages(ctx, client, prompt, skillsRegistry)
	if err != nil {
		return err
	}

	msg, err := chat.SendCompletion(ctx, client, messages, toolsRegistry)
	if errors.Is(context.Cause(ctx), errRunTimeout) {
		return fmt.Errorf("%w after %v", errRunTimeout, d)
	}
	if err != nil {
		return err
	}

	_, err = io.WriteString(stdout, msg)
	return err
}

func exitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}

	fmt.Fprintf(stderr, "app: %v\n", err)
	if errors.Is(err, context.Canceled) {
		return exitCodeInterrupted
	}

	return 1
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)

	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	exit(exitCode(err, os.Stderr))
}
