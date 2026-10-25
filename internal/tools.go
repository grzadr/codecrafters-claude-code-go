package tools

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Parameter struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Function struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Parameters  Parameter `json:"parameters"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// ToParam converts the static Tool struct into the OpenAI SDK parameter type.
func (t Tool) ToParam() openai.ChatCompletionToolUnionParam {
	props := make(map[string]any, len(t.Function.Parameters.Properties))
	for k, v := range t.Function.Parameters.Properties {
		props[k] = map[string]any{
			"type":        v.Type,
			"description": v.Description,
		}
	}

	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        t.Function.Name,
		Description: openai.String(t.Function.Description),
		Parameters: shared.FunctionParameters{
			"type":       t.Function.Parameters.Type,
			"properties": props,
			"required":   t.Function.Parameters.Required,
		},
	})
}

var ReadTool = Tool{
	Type: "function",
	Function: Function{
		Name:        "Read",
		Description: "Read and return the contents of a file",
		Parameters: Parameter{
			Type: "object",
			Properties: map[string]Property{
				"file_path": {
					Type:        "string",
					Description: "The path to the file to read",
				},
			},
			Required: []string{"file_path"},
		},
	},
}

func readFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

type ReadArgs struct {
	FilePath string `json:"file_path"`
}

func ReadFunc(rawArgs string) ([]byte, error) {
	var args ReadArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, err
	}

	return readFile(args.FilePath)
}

var ToolsRegistry = map[string]func(rawArgs string) ([]byte, error){
	"Read": ReadFunc,
}

func Execute(name string, rawArgs string) ([]byte, error) {
	fn, ok := ToolsRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return fn(rawArgs)
}

var ToolsList = []openai.ChatCompletionToolUnionParam{
	ReadTool.ToParam(),
}
