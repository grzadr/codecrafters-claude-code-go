package chat

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/codecrafters-io/claude-code-starter-go/internal/skills"
	"github.com/codecrafters-io/claude-code-starter-go/internal/tools"
)

func NewOpenAiClient() openai.Client {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		panic("Env variable OPENROUTER_API_KEY not found")
	}

	baseURL := cmp.Or(os.Getenv("OPENROUTER_BASE_URL"), "https://openrouter.ai/api/v1")

	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))
}

const (
	skillPrefix = "/"
	maxTurns    = 50
)

var argumentRegExp = regexp.MustCompile(`\$(?:ARGUMENTS\b(?:\[(\d+)\])?|(\d+))`)

func getSkillNames(prompt string, registry skills.SkillRegistry) ([]skills.Skill, []string) {
	fields := strings.Fields(prompt)
	found := make([]skills.Skill, 0, len(fields))
	for i, f := range fields {
		name, hasPrefix := strings.CutPrefix(f, skillPrefix)
		skill, ok := registry[name]
		if !hasPrefix || !ok {
			return found, fields[i:]
		}
		found = append(found, skill)
	}

	return found, nil
}

func parsePrompt(prompt string, skills skills.SkillRegistry) []string {
	namesSkills, args := getSkillNames(prompt, skills)
	if len(namesSkills) == 0 {
		return []string{prompt}
	}

	userPrompts := make([]string, 0, len(namesSkills))
	for _, s := range namesSkills {
		out := argumentRegExp.ReplaceAllStringFunc(s.Msg(), func(m string) string {
			snum := strings.TrimRight(strings.TrimLeft(m, "$ARGUMENTS["), "]")
			if snum == "" {
				return strings.Join(args, " ")
			}

			num, err := strconv.Atoi(snum)
			if err != nil || num >= len(args) {
				return ""
			}

			return args[num]
		})

		userPrompts = append(userPrompts, out)
	}

	return userPrompts
}

func BuildMessages(
	prompt string,
	registry skills.SkillRegistry,
) []openai.ChatCompletionMessageParamUnion {
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

	for _, m := range parsePrompt(prompt, registry) {
		messages = append(messages, openai.UserMessage(m))
	}

	return messages
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
			return msg.Content, nil
		}

		messages = append(messages, msg.ToParam())

		for _, tc := range msg.ToolCalls {
			res, execErr := reg.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if execErr == nil && res.Fork {
				res.Output, execErr = SendCompletion(
					ctx,
					client,
					[]openai.ChatCompletionMessageParamUnion{openai.UserMessage(res.Output)},
					reg,
				)
			}

			if execErr != nil {
				res.Output = fmt.Sprintf("error: %v\n", execErr)
				fmt.Fprintln(os.Stderr, res.Output)
			}
			messages = append(messages, openai.ToolMessage(res.Output, tc.ID))
		}
	}

	return "", fmt.Errorf("error: gave up after %d turns", maxTurns)
}
