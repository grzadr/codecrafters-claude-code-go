package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"

	chat "github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	tools "github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

var exitFunc = os.Exit

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

	messages := chat.BuildMessages(args.Prompt)

	ctx := context.Background()

	for {
		resp, err := client.Chat.Completions.New(ctx,
			openai.ChatCompletionNewParams{
				Model:    "anthropic/claude-haiku-4.5",
				Messages: messages,
				Tools:    tools.ToolsList,
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

		// // You can use print statements as follows for debugging, they'll be visible when running tests.
		// fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

		msg := resp.Choices[0].Message
		messages = append(messages, msg.ToParam())
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				data, err := tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					exitFunc(1)
					return
				}
				messages = append(messages, openai.ToolMessage(string(data), tc.ID))
			}

		} else {
			break
		}
	}

	fmt.Print(messages[len(messages)-1].OfAssistant.Content.OfString.Value)

}
