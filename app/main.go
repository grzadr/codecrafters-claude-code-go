package main

import (
	"context"
	"flag"
	"os"

	chat "github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	skills "github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	tools "github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

const (
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
	toolsRegistry := tools.NewToolsRegistry(skillsRegistry)

	messages := chat.BuildMessages(args.Prompt, skillsRegistry)

	ctx := context.Background()

	msg, err := chat.SendCompletion(ctx, client, messages, toolsRegistry)
	if err != nil {
		panic(err)
	}

	_, err = os.Stdout.WriteString(msg)
	if err != nil {
		panic(err)
	}
}
