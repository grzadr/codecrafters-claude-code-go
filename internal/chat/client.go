package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	"github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

func NewOpenAiClient() (openai.Client, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return openai.Client{}, errors.New("OPENROUTER_API_KEY is not set")
	}

	baseURL := cmp.Or(os.Getenv("OPENROUTER_BASE_URL"), "https://openrouter.ai/api/v1")

	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL)), nil
}

const (
	skillPrefix = "/"
	maxTurns    = 50
)

func getSkillNames(prompt string, registry skills.SkillRegistry) ([]skills.Skill, string) {
	var found []skills.Skill
	rest := prompt
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			end = len(rest)
		}

		name, hasPrefix := strings.CutPrefix(rest[:end], skillPrefix)
		skill, ok := registry[name]
		if rest == "" || !hasPrefix || !ok {
			return found, rest
		}

		found = append(found, skill)
		rest = rest[end:]
	}
}

func runFork(ctx context.Context, client openai.Client, prompt string) (string, error) {
	return SendCompletion(
		ctx,
		client,
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage(prompt)},
		tools.NewToolsRegistry(nil),
	)
}

func parsePrompt(
	ctx context.Context,
	client openai.Client,
	prompt string,
	reg skills.SkillRegistry,
) ([]string, error) {
	namesSkills, args := getSkillNames(prompt, reg)
	if len(namesSkills) == 0 {
		return []string{prompt}, nil
	}

	userPrompts := make([]string, 0, len(namesSkills))
	for _, s := range namesSkills {
		msg := s.Msg(args)
		if s.Fork() {
			answer, err := runFork(ctx, client, msg)
			if err != nil {
				return nil, fmt.Errorf("skill %s: %w", s.Name, err)
			}
			msg = fmt.Sprintf("Skill %s ran in a subagent and returned:\n\n%s", s.Name, answer)
		}
		userPrompts = append(userPrompts, msg)
	}

	return userPrompts, nil
}

func BuildMessages(
	ctx context.Context,
	client openai.Client,
	prompt string,
	registry skills.SkillRegistry,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	var messages []openai.ChatCompletionMessageParamUnion

	if len(registry) > 0 {
		var systemMsg strings.Builder
		systemMsg.WriteString("You have access to the following skills:\n\n")

		for _, s := range slices.Sorted(maps.Keys(registry)) {
			skill := registry[s]
			fmt.Fprintf(&systemMsg, "- %s: %s\n", skill.Name, skill.Description)
		}

		fmt.Fprint(
			&systemMsg,
			"\n\nIf a skill matches the user's request, call the Skill tool with its name and follow the instructions it returns.\n\n",
		)

		messages = append(messages, openai.SystemMessage(systemMsg.String()))
	}

	prompts, err := parsePrompt(ctx, client, prompt, registry)
	if err != nil {
		return nil, err
	}

	for _, m := range prompts {
		messages = append(messages, openai.UserMessage(m))
	}

	return messages, nil
}

func textParts(raw string) string {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(raw), &parts)
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func finalAnswer(choice openai.ChatCompletionChoice) (string, error) {
	content := choice.Message.Content
	if strings.HasPrefix(choice.Message.JSON.Content.Raw(), "[") {
		content = textParts(content)
	}

	switch {
	case content != "":
		return content, nil
	case choice.Message.Refusal != "":
		return "", fmt.Errorf("model refused: %s", choice.Message.Refusal)
	default:
		return "", fmt.Errorf("empty reply (finish_reason %q)", choice.FinishReason)
	}
}

func SendCompletion(
	ctx context.Context,
	client openai.Client,
	messages []openai.ChatCompletionMessageParamUnion,
	reg tools.Registry,
) (string, error) {
	toolsParams := reg.Params()
	for range maxTurns {
		resp, err := client.Chat.Completions.New(ctx,
			openai.ChatCompletionNewParams{
				Model:    "anthropic/claude-haiku-4.5",
				Messages: messages,
				Tools:    toolsParams,
			},
		)
		if err != nil {
			return "", fmt.Errorf("chat completion: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("no choices in response")
		}

		msg := resp.Choices[0].Message

		if len(msg.ToolCalls) == 0 {
			return finalAnswer(resp.Choices[0])
		}

		messages = append(messages, msg.ToParam())

		for _, tc := range msg.ToolCalls {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			res, execErr := reg.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if execErr == nil && res.Fork {
				res.Output, execErr = runFork(ctx, client, res.Output)
			}

			if execErr != nil {
				errLine := fmt.Sprintf("error: %v\n", execErr)
				res.Output += errLine
				fmt.Fprintln(os.Stderr, errLine)
			}
			messages = append(messages, openai.ToolMessage(res.Output, tc.ID))
		}
	}

	return "", fmt.Errorf("gave up after %d turns", maxTurns)
}
