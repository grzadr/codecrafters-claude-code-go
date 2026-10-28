package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"

	chat "github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	skills "github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	tools "github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

//nolint:gochecknoglobals // injectable exit hook for testing main
var exitFunc = os.Exit

// maxTurns caps model round trips so a model that keeps calling tools cannot loop forever.
const (
	maxTurns   = 50
	skillsPath = ".claude/skills"
)

type mainArgs struct {
	Prompt string
}

func newMainArgs(args []string) mainArgs {
	var parsed mainArgs

	fs := flag.NewFlagSet("app", flag.ExitOnError)
	fs.StringVar(&parsed.Prompt, "p", "", "Prompt to send to LLM")
	_ = fs.Parse(args)

	if parsed.Prompt == "" {
		panic("Prompt must not be empty")
	}

	return parsed
}

func main() {
	args := newMainArgs(os.Args[1:])

	client := chat.NewOpenAiClient()

	skillsRegistry := skills.InitSkillsRegistry(skillsPath)
	toolsRegistry, toolsList := tools.InitTools(skillsRegistry)

	messages := chat.BuildMessages(args.Prompt, skillsRegistry)

	ctx := context.Background()

	for range maxTurns {
		resp, err := client.Chat.Completions.New(ctx,
			openai.ChatCompletionNewParams{
				Model:    "anthropic/claude-haiku-4.5",
				Messages: messages,
				Tools:    toolsList,
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			exitFunc(1)
			return
		}
		if len(resp.Choices) == 0 {
			panic("No choices in response")
		}

		msg := resp.Choices[0].Message
		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			_, _ = os.Stdout.WriteString(msg.Content)
			return
		}

		for _, tc := range msg.ToolCalls {
			data, execErr := toolsRegistry.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if execErr != nil {
				errMsg := fmt.Sprintf("error: %v\n", execErr)
				fmt.Fprintln(os.Stderr, errMsg)
				messages = append(messages, openai.ToolMessage(errMsg, tc.ID))
				continue
			}
			messages = append(messages, openai.ToolMessage(string(data), tc.ID))
		}
	}

	fmt.Fprintf(os.Stderr, "error: gave up after %d turns\n", maxTurns)
	exitFunc(1)
}
