package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	tools "github.com/codecrafters-io/claude-code-starter-go/internal"
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

func newOpenAiClient() openai.Client {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	baseUrl := os.Getenv("OPENROUTER_BASE_URL")
	if baseUrl == "" {
		baseUrl = "https://openrouter.ai/api/v1"
	}

	if apiKey == "" {
		panic("Env variable OPENROUTER_API_KEY not found")
	}

	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseUrl))
}

func main() {
	args := newMainArgs(os.Args[1:])

	client := newOpenAiClient()
	messages := []openai.ChatCompletionMessageParamUnion{
		{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String(args.Prompt),
				},
			},
		},
	}

	for {
		resp, err := client.Chat.Completions.New(context.Background(),
			openai.ChatCompletionNewParams{
				Model:    "anthropic/claude-haiku-4.5",
				Messages: messages,
				Tools:    tools.ToolsList,
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(resp.Choices) == 0 {
			panic("No choices in response")
		}

		// You can use print statements as follows for debugging, they'll be visible when running tests.
		fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) > 0 {
			tc := msg.ToolCalls[0]
			if err := tools.Execute(tc.Function.Name, tc.Function.Arguments); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Print(msg.Content)
		}
	}

}
