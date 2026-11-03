# Implementation Details

Detailed breakdown of the design choices, algorithms, and testing patterns in each package.

---

## 1. Skill Discovery & Substitution (`internal/skills`)

Defined in [`internal/skills/skills.go`](../internal/skills/skills.go).

### Frontmatter Parsing (`newSkill`)
Each skill resides in `.claude/skills/<folder>/SKILL.md` and begins with a `---` YAML frontmatter block followed by markdown instructions:
1. `bytes.CutPrefix(content, []byte("---\n"))` verifies the opening delimiter.
2. `bytes.Cut(content, []byte("---\n"))` splits the YAML header from the markdown body on the first closing delimiter, so `---` horizontal rules inside the markdown body are never misparsed as YAML.
3. `yaml.Unmarshal(header, &s)` populates `Name`, `Description`, and `Context`. `s.Path` stores the skill directory path (not `SKILL.md` itself).

### String Substitutions (`Skill.Msg`)
[`Skill.Msg(args ...string)`](../internal/skills/skills.go) renders a skill into the user/tool message format:

```text
Skill: <name> (located at <path>)
Paths in the instructions below are relative to that folder.

<expanded body>
```

Substitution runs on `strings.TrimSpace(s.Body)` via a single compiled regular expression before wrapping the header:

```go
var subRegExp = regexp.MustCompile(
	`\$\{CLAUDE_SKILL_DIR\}|\$ARGUMENTS(?:\[[^\]\s]*\]?|\w+)?|\$\d\w*`,
)
```

| Token | Behavior | Edge-Case Handling |
| :--- | :--- | :--- |
| `${CLAUDE_SKILL_DIR}` | Expands to `s.Path` | Does not count as an argument placeholder |
| `$ARGUMENTS` | Expands to `strings.Join(args, " ")` | Trailing word chars (`$ARGUMENTSX`, `$ARGUMENTS_x`) or malformed brackets (`$ARGUMENTS[0`) match the longer token and stay literal |
| `$ARGUMENTS[N]` | Expands to `args[N]` (0-indexed) | Validated with `strconv.ParseUint(idxStr, 10, 0)`: syntax errors (`$ARGUMENTS[x]`) stay literal; out-of-range or overflow indices expand to `""` |
| `$N` | Shorthand for `$ARGUMENTS[N]` | Trailing word chars (`$0x`) fail `ParseUint` with `strconv.ErrSyntax` and stay literal |
| No argument placeholder | Appends `\n\nARGUMENTS: <joined args>` | Only appended when `len(args) > 0` and no `$ARGUMENTS` / `$N` placeholder matched |

---

## 2. Prompt Parsing & Agentic Loop (`internal/chat`)

Defined in [`internal/chat/client.go`](../internal/chat/client.go).

### Stacked Slash Commands (`getSkillNames` & `parsePrompt`)
Users can invoke one or more skills at the start of a prompt (e.g. `/apple /grape arg1 arg2`):
- `getSkillNames` splits the prompt with `strings.Fields(prompt)` and consumes leading `/name` tokens as long as `name` exists in `SkillRegistry`.
- The first token that either lacks a `/` prefix or is not a registered skill ends skill consumption; that token and all remaining tokens become `args` shared by every invoked skill.
- Each matched skill becomes a separate `UserMessage(s.Msg(args...))` in prompt order. If the first token is not a known `/skill`, the raw prompt is sent as a single `UserMessage`.

### System Prompt Progressive Disclosure (`BuildMessages`)
When `len(registry) > 0`, `BuildMessages` prepends a `SystemMessage` listing only **level-1 metadata** (`- <name>: <description>`) sorted alphabetically by skill name (`slices.Sorted(maps.Keys(registry))`), without leaking skill bodies into the system prompt.

### Multi-Turn Completion & Forked Subagents (`SendCompletion`)
- Loops up to `maxTurns = 50`, sending `messages` and `reg.Params()` to `anthropic/claude-haiku-4.5`.
- When the assistant returns `ToolCalls`, each call is dispatched via `reg.Execute(ctx, tc.Function.Name, tc.Function.Arguments)`.
- **Subagent Forking (`context: fork`)**: If `res.Fork` is `true` (case-insensitive `context: fork` in frontmatter), `SendCompletion` recursively invokes itself with:
  - A fresh message slice containing only `[openai.UserMessage(res.Output)]` (isolating the main conversation history).
  - `tools.NewToolsRegistry(nil)` so the subagent has `Read`, `Write`, and `Bash` (plus `Skill` with an empty skill map), preventing infinite skill-fork recursion.
  - The subagent's final text answer replaces `res.Output` as the `ToolMessage` returned to the parent conversation.

---

## 3. Tool Registry & Process Lifecycle (`internal/tools`)

Defined in [`internal/tools/tools.go`](../internal/tools/tools.go).

### Generic Type-Safe Tool Dispatch (`withArgs[T]`)
Instead of repeating `json.Unmarshal` boilerplate in every tool handler, `withArgs[T]` wraps strongly typed handlers into a uniform `ToolFunc`:

```go
func withArgs[T any](fn func(context.Context, T) (Result, error)) ToolFunc
```

### Registered Tools

| Tool | Args Struct | Implementation |
| :--- | :--- | :--- |
| `Read` | `readArgs{FilePath}` | `os.ReadFile(args.FilePath)` |
| `Write` | `writeArgs{FilePath, Content}` | `os.WriteFile(args.FilePath, ..., 0o600)` |
| `Bash` | `bashArgs{Command}` | `runCommand(ctx, []string{"sh", "-c", args.Command}, 100*time.Second)` |
| `Skill` | `skillArgs{Name, Args}` | Looks up `s[args.Name]` and returns `Result{Output: skill.Msg(strings.Fields(args.Args)...), Fork: skill.Fork()}` |

### Graceful Subprocess Termination (`runCommand`)
`runCommand` configures `exec.CommandContext(ctx, ...)` with:
- `cmd.Cancel`: sends `syscall.SIGTERM` instead of the default `SIGKILL` when `ctx` is canceled.
- `cmd.WaitDelay = gracePeriod`: waits up to `gracePeriod` (`100s` for `Bash`) before escalating to `SIGKILL` and closing I/O pipes.
- Non-zero exits are wrapped in `*ProcessError{Code, Err}` using Go 1.26 `errors.AsType[*exec.ExitError](err)` (`Code = -1` if the binary failed to start).

---

## 4. Testing Architecture

- **Black-Box External Test Packages**: `internal/chat`, `internal/skills`, and `internal/tools` use `package <name>_test` to test against the public API.
- **Unexported Symbol Bridges (`export_test.go`)**: Unexported helpers requiring direct unit tests (`parsePrompt` in [`internal/chat/export_test.go`](../internal/chat/export_test.go), `runCommand` in [`internal/tools/export_test.go`](../internal/tools/export_test.go)) are exposed only during `go test` compilation without polluting the production package API.
- **Zero-Network Integration Tests ([`app/main_test.go`](../app/main_test.go))**: Overrides `http.DefaultTransport` with a `roundTripFunc` adapter backed by `httptest.NewRecorder()` and isolates filesystem state with `t.TempDir()` + `t.Chdir()`.

