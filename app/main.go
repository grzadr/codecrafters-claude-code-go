package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/codecrafters-io/claude-code-starter-go/internal/chat"
	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	"github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

func newMainArgs(args []string) string {
	var prompt string

	fs := flag.NewFlagSet("app", flag.ExitOnError)
	fs.StringVar(&prompt, "p", "", "Prompt to send to LLM")
	_ = fs.Parse(args)

	if prompt == "" {
		panic("Prompt must not be empty")
	}

	return prompt
}

func main() {
	prompt := newMainArgs(os.Args[1:])

	client := chat.NewOpenAiClient()

	skillsRegistry := skills.InitSkillsRegistry(".claude/skills")
	toolsRegistry := tools.NewToolsRegistry(skillsRegistry)

	messages := chat.BuildMessages(prompt, skillsRegistry)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	msg, err := chat.SendCompletion(ctx, client, messages, toolsRegistry)
	if err != nil {
		panic(err)
	}

	if _, err = os.Stdout.WriteString(msg); err != nil {
		panic(err)
	}
}
