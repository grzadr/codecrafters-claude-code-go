# Fix plan

Known bugs and small hardening items in this repo, each checked as still present at the
file:line given in its section.

Effort: **S** < 1 hour, a few lines · **M** about half a day, several files and tests ·
**L** a day or more, structural. Nothing came out as L.

## Summary

| Effort | ID | Fix | Area | Depends on |
|---|---|---|---|---|
| S | 0001 | Keep Bash stdout/stderr when the command fails (`res.Output +=`, not `=`) | chat | – |
| S | 0002 | Write with a missing or misnamed `content` key returns an error instead of truncating the file | tools | – |
| S | 0006 | Stop queued tool calls after Ctrl-C (`ctx.Err()` check in the tool loop) | chat/tools | – |
| S | 0009 | Match the closing `---` as a whole line, not a substring | skills | – |
| S | 0010 | Accept CRLF, a UTF-8 BOM, and a missing final newline in frontmatter | skills | 0009 |
| S | 0011 | Follow symlinked skill directories (`os.Stat`, not `IsDir`) | skills | – |
| S | 0012 | A skill with no `name` falls back to its directory name | skills | – |
| S | 0013 | `ErrWaitDelay` after a clean shell exit counts as success | tools | 0003 |
| S | 0014 | Run `bash -c` when bash is on PATH, otherwise `sh -c` | tools | – |
| S | 0015 | Add a trailing newline to the final answer ⚠ tester risk | main | – |
| S | 0016 | A refusal or empty reply is an error, not a silent exit 0 | chat | – |
| S | 0016b | Print only the text parts of an array-shaped final answer | chat | 0016 |
| S | 0017 | Duplicate skill names: keep the first and warn | skills | (0029) |
| S | 0018 | Remove `OPENROUTER_API_KEY` from the Bash child's environment | tools | – |
| S | 0021 | Fixed 2-minute per-command Bash timeout | tools | 0003 |
| S | 0004 | Return errors instead of panicking; exit 1, or 130 on Ctrl-C | main/chat | – |
| S | 0005 | Reject extra words after `-p` (`quote the prompt`) | main | 0004 |
| S–M | 0019 | Overall run timeout (`WithTimeoutCause`) | main | 0004, soft 0003 |
| S–M | 0029 | `log/slog` on stderr in place of the `Fprintf` diagnostics | all | – |
| M | 0003 | Bash in its own process group, SIGTERM to the group, 5 s grace, second Ctrl-C exits | tools/main | – |
| M | 0007 | A fork skill invoked by /slash runs as a fork, not inline | skills/chat | 0008 |
| M | 0008 | Keep slash-command arguments verbatim for `$ARGUMENTS` | skills | – |
| M | 0028 | Cobra/Viper root command (`-p/--prompt`, env binding) ⚠ new dependencies | main | 0004, 0005 |

## Suggested order

1. **One batch of quick wins (all S, independent):** 0001, 0002, 0006, 0009+0010, 0011, 0012,
   0014, 0018, 0016+0016b. Most of these are one-file changes, each with one or two table test rows.
2. **Errors and exit codes:** 0004, then 0005. The later CLI items build on these.
3. **Process control:** 0003, then 0013 and 0021, which reuse its process-group and `WaitDelay` setup.
   Then 0019.
4. **Skills behaviour:** 0008, then 0007 (`BuildMessages` gets `ctx` and a client, so many test call
   sites change). 0017 can go in with step 1, using `Fprintf`, or after 0029, using slog.
5. **Optional, decide first:** 0015 (stdout format), 0029, 0028.

## Risks for the CodeCrafters tester

The tester runs `your_program.sh -p "<prompt>"` and builds from `./app`. Every plan keeps that
working. Check these with the tester before you merge:

- **0015**: the tester may compare stdout exactly, so a trailing newline could fail a stage. Drop
  this item if it does.
- **0008 / 0007**: a stage may expect collapsed whitespace in `$ARGUMENTS`, or the current inline
  slash behaviour.
- **0004 / 0005 / 0016**: these change the exit codes and stderr text. Nobody has checked whether any stage
  asserts on them.
- **0028**: adds `spf13/cobra` and `spf13/viper` to the CodeCrafters build. Decide whether this repo
  needs it at all.

---

# Details per item

## Tools area (internal/tools): Bash / Read / Write

Every item edits `internal/tools/tools.go` (201 lines) except 0006 (chat loop) and part of 0003 (`app/main.go`).
Suggested order: 0002, 0014, 0018 (independent one-liners) -> 0003 -> 0013 -> 0006 -> 0021.

### 0002 Write with a missing or misnamed `content` key truncates the file to 0 bytes
- **Effort:** S
- **Location:** `internal/tools/tools.go:71-78`. `writeArgs.Content` is a plain `string`, so `{"file_path":"x","contents":"..."}` writes `""` and returns success.
- **Plan:** change `Content string` to `Content *string`. In `writeFunc`, `if args.Content == nil { return Result{}, errors.New(`missing required argument "content"`) }`, then write `*args.Content`. Keep decoding lenient: no `DisallowUnknownFields`. Don't add a check for Bash `command`.
- **Tests:** in `internal/tools/tools_test.go`, add `TestWrite_ContentArgument` with three table cases: a missing key and a misnamed `contents` key both return the error and leave the existing file unchanged, and `"content":""` creates an empty file.
- **Depends on:** none.

### 0003 Ctrl-C/SIGTERM does not stop a running Bash command (100s hang, orphans, second Ctrl-C ignored)
- **Effort:** M
- **Location:** `internal/tools/tools.go:102-104`: `cmd.Cancel` signals only the `sh` PID and there is no `Setpgid`. `tools.go:130` sets `bashGracePeriod = 100 * time.Second`. `app/main.go:39-40`: `signal.NotifyContext` stays registered until main returns, so a second Ctrl-C is swallowed.
- **Plan:**
  1. Add `internal/tools/proc_unix.go` (`//go:build unix`). Its `setProcessGroup(cmd)` sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` and `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }`. Add a `shortcut:` comment: SIGKILL after WaitDelay reaches only the group leader.
  2. Add `proc_windows.go` with an empty `setProcessGroup` (optional, because CodeCrafters runs on Linux; the file costs nothing and keeps `go build` working on Windows).
  3. In `tools.go:101-104`, replace the inline `cmd.Cancel` with `setProcessGroup(cmd)` and drop the `syscall` import.
  4. Set `tools.go:130` `bashGracePeriod` to `5 * time.Second`.
  5. In `app/main.go`, after `defer stop()` at line 40, add `context.AfterFunc(ctx, stop)`.
- **Tests:** in `TestRunCommand`, add the subtest "cancel stops the shell's children": run `RunCommand(ctx, {"sh","-c","sleep 30; true"}, 30*time.Second)` and cancel after about 100ms. It must return an error within 5s. Before the fix it hangs for 30s. The existing `TestBash` "context cancellation" subtest keeps passing. The Windows file has no statements, so coverage stays 100%. The `AfterFunc` line in `main.go` is covered because `main_test.go` runs `main()`. Check with `go test -cover ./app`.
- **Depends on:** none. 0013 and 0021 build on the shorter grace period and the group kill.
- **CodeCrafters risk:** low. Setpgid only changes how signals are delivered, and the tester never sends a signal.

### 0006 After Ctrl-C, queued Read/Write tool calls in the same turn still run
- **Effort:** S
- **Location:** `internal/chat/client.go:125-126`: the `for _, tc := range msg.ToolCalls` loop never checks `ctx.Err()`.
- **Plan:** at the top of the loop body, `if ctx.Err() != nil { return "", ctx.Err() }`.
- **Tests:** add `TestSendCompletion_CancelStopsQueuedToolCalls` in `internal/chat/completion_test.go`, using `fakeModel`. One assistant message has two tool calls: a Bash `sleep 2`, then a Write to a temp file. A goroutine calls `cancel()` while the sleep runs (for example `time.AfterFunc(50*time.Millisecond, cancel)`). Assert that `SendCompletion` returns `context.Canceled` and the temp file does not exist. `toolCallReply` builds only one call, so write this reply body inline.
- **Depends on:** none (0003 only makes the cancel fast).

### 0013 A Bash command that starts a background process stalls 100s, then fails and drops its output
- **Effort:** S
- **Location:** `internal/tools/tools.go:109-121`. `CombinedOutput` returns `exec.ErrWaitDelay` while a background child holds the pipe, and the code wraps that error as `ProcessError{Code:-1}`.
- **Plan:** right after `out, err := cmd.CombinedOutput()`, add `if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() { return string(out), nil }`, with a `shortcut:` comment that each background start still costs the grace period.
- **Tests:** in `TestRunCommand`, add the subtest "background child does not fail a successful command": run `RunCommand(ctx, {"sh","-c","sleep 5 & echo started"}, time.Second)`. It returns `"started\n"` with a nil error in under 2s.
- **Depends on:** 0003 (the 5s grace period). Without it, every background start still costs 100s.

### 0014 The tool named Bash runs `sh -c` (dash on Debian), so bashisms fail with exit 127
- **Effort:** S
- **Location:** `internal/tools/tools.go:133`: `[]string{"sh", "-c", args.Command}`.
- **Plan:** `shell := "sh"; if _, err := exec.LookPath("bash"); err == nil { shell = "bash" }`, then pass `shell` in place of `"sh"`. The tool name, description and schema stay the same.
- **Tests:** add `TestBash_Environment` in `tools_test.go`. Subtest "runs bash": `[[ -d / ]] && echo {1..2}` returns `"1 2\n"`. Subtest "falls back to sh": `t.Setenv("PATH", <temp dir holding only an sh symlink>)`, so that both branches are covered.
- **Depends on:** none.
- **CodeCrafters risk:** low. The tester image has bash. The fallback keeps sh-only images working.

### 0018 The API key is inherited by every Bash child, so a prompt injection can exfiltrate it
- **Effort:** S
- **Location:** `internal/tools/tools.go:99`. `cmd.Env` is nil, so the child sees `OPENROUTER_API_KEY` (read at `internal/chat/client.go:21`).
- **Plan:** after `exec.CommandContext`, add `cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "OPENROUTER_API_KEY=") })`. Add a `shortcut:` comment that `/proc/$PPID/environ` still exposes the key.
- **Tests:** in a `TestBash_Environment` subtest, `t.Setenv("OPENROUTER_API_KEY","test-key")` and run Bash `printenv OPENROUTER_API_KEY; echo done`. The output is `"done\n"`, so the key is absent. Use a plain value, not a realistic key format (gitleaks).
- **Depends on:** none.
- **CodeCrafters risk:** none. The tester supplies the key to the CLI, not to the shell commands.

### 0021 No per-command timeout on Bash, so a server, a watcher or `yes` blocks forever
- **Effort:** S
- **Location:** `internal/tools/tools.go:132-134`. `bashFunc` uses the signal-only ctx, which has no deadline.
- **Plan:** add `const bashTimeout = 2 * time.Minute` and `var errCommandTimeout = errors.New("command timed out")`. Give `bashFunc` a `timeout time.Duration` parameter, and in it: `ctx, cancel := context.WithTimeoutCause(ctx, timeout, errCommandTimeout); defer cancel()`. After `runCommand`, `if errors.Is(context.Cause(ctx), errCommandTimeout) { return Result{Output: out}, fmt.Errorf("%w after %v", errCommandTimeout, timeout) }`. Because `withArgs` needs `func(ctx, bashArgs)`, register Bash as `withArgs(func(ctx context.Context, a bashArgs) (Result, error) { return bashFunc(ctx, a, bashTimeout) })`. Keep `NewToolsRegistry(s)` unchanged. Add a bridge `BashFunc(ctx, cmd string, timeout time.Duration)` in `export_test.go`, so tests don't wait 2 minutes.
- **Tests:** add `TestBash_Timeout` with two subtests. "Command timeout keeps the output so far": `echo started; sleep 5` with a 100ms timeout returns `"started\n"` and the error `command timed out after 100ms`. "Parent deadline is not a command timeout": the parent ctx deadline is 100ms and the timeout is large, and the error does not contain "command timed out".
- **Depends on:** 0003 (the group kill plus the 5s grace period, or a timed-out `sleep` grandchild holds the pipe for 100s).
- **Out of scope:** a flag to configure the timeout. Keep the `-p` CLI unchanged.
- **CodeCrafters risk:** low. No tester stage runs commands near 2 minutes.

## Chat loop and final answer (internal/chat, app/main.go)

Tests use the existing `fakeModel` / `sequence` / `contentReply` / `toolCallReply` helpers in
`internal/chat/completion_test.go` and the table in `TestSendCompletion` (line 120).

### 0001 Failing Bash command's stdout/stderr never reach the model

- **Effort:** S
- **Location:** `internal/chat/client.go:137`, `res.Output = fmt.Sprintf("error: %v\n", execErr)`
  overwrites the CombinedOutput that `runCommand` (`internal/tools/tools.go:109-117`) returns with the `*ProcessError`.
- **Plan:** in `client.go:136-139`, build `errLine := fmt.Sprintf("error: %v\n", execErr)`, then
  `res.Output += errLine` and `fmt.Fprintln(os.Stderr, errLine)`. The output stays first and the error line comes after it. Only the
  error line goes to stderr.
- **Tests:** a new `TestSendCompletion` row named "failing command keeps its output and appends the error". The script is
  `toolCallReply("t1","Bash",`{"command":"echo out; echo err >&2; exit 3"}`)`, then `contentReply("done")`.
  It expects `wantTool: "out\nerr\nerror: command exited with code 3: exit status 3\n"` and `wantRequests: 2`.
  The existing "tool error is sent back" row (Unknown tool, empty output) still passes unchanged.
- **Depends on:** none. A fork's `res.Output` is the skill body; when the fork fails, `+=` keeps that body in front of the
  error. That is acceptable.

### 0015 Final answer is written without a trailing newline

- **Effort:** S
- **Location:** `app/main.go:47`, `os.Stdout.WriteString(msg)` with no newline.
- **Plan:** before `app/main.go:47`, add
  `if !strings.HasSuffix(msg, "\n") { msg += "\n" }` and import `strings`.
- **Tests:** `app/main_test.go:98`: change the expectation to `"done\n"`. Add one case where the answer already
  ends in `\n` (for example a scripted `"done\n"`) and assert that exactly one newline is printed.
- **Depends on:** none.
- **CodeCrafters risk:** this is the only item that changes stdout byte for byte. If the tester compares stdout
  exactly with no trimming, the extra `\n` could fail a stage. Run the tester (`codecrafters test`) after this
  change. If it fails, drop this item.

### 0016 Empty final answer (refusal) exits 0 with nothing printed

- **Effort:** S
- **Location:** `internal/chat/client.go:119-121`, `return msg.Content, nil`, even when Content is
  empty. Refusal and FinishReason are ignored.
- **Plan:** add the helper and call it from `client.go:120` as `return finalAnswer(resp.Choices[0])`:
  ```go
  func finalAnswer(choice openai.ChatCompletionChoice) (string, error) {
  	switch {
  	case choice.Message.Content != "": return choice.Message.Content, nil
  	case choice.Message.Refusal != "": return "", fmt.Errorf("model refused: %s", choice.Message.Refusal)
  	default: return "", fmt.Errorf("empty reply (finish_reason %q)", choice.FinishReason)
  	}
  }
  ```
  Content cut off by `length` still counts as an answer. `main` already panics on the error, so the run exits non-zero.
- **Tests:** two `TestSendCompletion` rows. The first, "refusal is an error", uses the body
  `{"choices":[{"message":{"role":"assistant","content":"","refusal":"no"}}]}` and `wantErr: "model refused: no"`.
  The second, "empty reply is an error", uses `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}]}`
  and `wantErr: `empty reply (finish_reason "length")``. No export bridge is needed, because both go through SendCompletion.
- **Depends on:** none. 0016b edits this helper.

### 0016b Print only the text parts of array-shaped final answers

- **Effort:** S
- **Location:** `internal/chat/client.go:120`. When a gateway sends `content` as an array of typed parts,
  openai-go keeps the raw JSON string in `Message.Content`, and the program prints that JSON verbatim.
- **Plan:** at the top of `finalAnswer` (from 0016), set `content := choice.Message.Content`, and
  `if strings.HasPrefix(choice.Message.JSON.Content.Raw(), "[") { content = textParts(content) }`. Then switch on
  `content`. `textParts` unmarshals `[]struct{Type, Text string}`, ignores the error (the SDK already parsed the JSON), and
  concatenates the `Text` of parts whose `Type == "text"`. Import `encoding/json`.
- **Tests:** three `TestSendCompletion` rows:
  1. A reasoning part plus two text parts (`"Verdict: OK\n"`, `"- detail"`) gives `"Verdict: OK\n- detail"`.
  2. Only a reasoning part, with `"finish_reason":"stop"`, gives `empty reply (finish_reason "stop")`.
  3. String content that looks like an array (`contentReply(`[{"type":"text","text":"x"}]`)`) is kept verbatim. This covers the
     Raw() check.
- **Depends on:** 0016, because it extends `finalAnswer`. Port both in one change.

### 0019 No overall run timeout

- **Effort:** S-M (one flag, one context wrapper, two tests in app/)
- **Location:** `app/main.go:39`, `signal.NotifyContext(context.Background(), ...)` with no deadline.
  `newMainArgs` (`app/main.go:15-27`) defines only `-p`.
- **Plan (stdlib `flag`, on top of 0004):**
  1. In `newMainArgs`, add `fs.DurationVar(&timeout, "timeout", 0, "Abort the run after this long (0 = no limit)")`
     and return `(prompt string, timeout time.Duration, err error)`.
  2. In `run`, after `signal.NotifyContext`: `if d > 0 { ctx, cancel = context.WithTimeoutCause(ctx, d, errRunTimeout); defer cancel() }`,
     with `var errRunTimeout = errors.New("run timed out")`.
  3. After `SendCompletion`: `if errors.Is(context.Cause(ctx), errRunTimeout) { return fmt.Errorf("%w after %v", errRunTimeout, d) }`.
     `exitCode` maps it to exit 1.
- **Tests:** `TestNewMainArgs` checks that `-p hi -timeout 50ms` returns 50ms and that the default is 0. `TestMainFunc` gets a subtest
  with `os.Args = {"app","-p","hi","-timeout","50ms"}` and a transport that blocks until `r.Context().Done()`.
  It asserts that the returned error (or stderr) contains `run timed out after 50ms` and that it returns in under 1s.
- **Depends on:** 0004 (the `run` / `exitCode` structure). 0003 is soft: it makes a Bash child's grandchildren die at the
  deadline too. Without it, `exec.CommandContext` still kills the direct child, so the core works. 0021 is optional;
  the cause check tells the two timeouts apart only when both exist.
- **CodeCrafters risk:** none. The flag is off by default, and the tester passes only `-p`.

### Suggested order

Port 0001, 0016 and 0016b together: they are one PR touching `internal/chat` only. Then port 0015 on its own, so that if the
tester fails, it can be reverted alone. Port 0019 last, after 0004 (and 0003 if that is being ported).

## Skill loading and slash commands

Items 0009, 0010, 0011 and 0012 all touch `newSkill` / `InitSkillsRegistry` in one file and
can go in one commit. 0007 and 0008 both change `parsePrompt`/`getSkillNames`, so do 0008 first.

### 0007 `context: fork` skill invoked by /slash runs inline

- **Effort:** M
- **Location:** `internal/chat/client.go:59`. `parsePrompt` appends `s.Msg(args...)`
  and never checks `s.Fork()`. Only the tool path forks (`client.go:127-134`).
- **Plan:**
  - Add `runFork(ctx, client, prompt)` in `client.go`. It calls `SendCompletion` with one
    `UserMessage` and `tools.NewToolsRegistry(nil)`. Use it at `client.go:128-133` as well.
  - `parsePrompt(ctx, client, prompt, reg) ([]string, error)`: when `s.Fork()`, run `runFork`
    and replace msg with `fmt.Sprintf("Skill %s ran in a subagent and returned:\n\n%s", s.Name, answer)`.
    Wrap the error as `skill %s: %w`.
  - `BuildMessages(ctx, client, prompt, reg) ([]Msg, error)`. In `app/main.go:37`, move the
    call below `signal.NotifyContext` (line 39) and `panic(err)` on error (or return it, after 0004).
  - `export_test.go`: `ParsePrompt` passes `context.Background(), openai.Client{}` and drops the error.
- **Tests:** update every `chat.BuildMessages(...)` call in `client_test.go` (lines 157-573) to
  the new signature. Add `TestBuildMessages_SlashForkSkill` in `completion_test.go`, using
  `fakeModel`: the subagent request holds only the body, and the main message is the answer.
  Add a failing-server case for the error branch. In `app/main_test.go`, give `runForkScenario`
  (line 347, prompt currently hard-coded) a `prompt string` parameter, then add
  `TestMainFunc_SlashForkSkill` (`runForkScenario(t, "/apple")`) and a failure-panics case.
  Both are needed to keep 100% coverage.
- **Depends on:** do 0008 first, since both edit `parsePrompt`.
- **CodeCrafters risk:** low. The prompt-only path (`-p "<text>"` with no skill) is unchanged.
  The extra request happens only for a slash-invoked fork skill.

### 0008 Slash-command arguments flattened by strings.Fields

- **Effort:** M
- **Location:** `internal/chat/client.go:37` (`strings.Fields(prompt)`), the joins
  at `internal/skills/skills.go:50,61`, and `internal/tools/tools.go:160` (`strings.Fields(args.Args)`).
- **Plan:**
  - `getSkillNames` returns `([]skills.Skill, string)`. In a loop, `TrimLeftFunc(rest, unicode.IsSpace)`,
    find the token end with `IndexFunc(rest, unicode.IsSpace)`, stop at the first non-skill
    token, and return the raw `rest`.
  - `Skill.Msg(args string)`: `args = strings.TrimSpace(args)`, `fields := strings.Fields(args)`.
    `$ARGUMENTS` and the appended `ARGUMENTS:` line use `args` verbatim, and `$N` and
    `$ARGUMENTS[N]` use `fields`. The guard becomes `args != ""`.
  - `tools.go:160`: `skill.Msg(args.Args)`. `parsePrompt` passes `args` (a string).
- **Tests:** `skills_test.go:189-232` `TestSkillMsg`: change `args []string` to `string` and add a
  multi-line or indented case. `client_test.go:293` "arguments whitespace collapsed" now expects the
  raw rest (`"a\t b"`), and `:413` "whitespace between stacked skills" expects `"x  y"`. Add a
  newline-preserving case. `tools_test.go:197,202`: `apple.Msg("")`, `apple.Msg("prod")`, and
  `:332` likewise.
- **Depends on:** none. Do it before 0007.
- **Out of scope:** a shell-style quote splitter; `$N` keeps `strings.Fields`.
- **CodeCrafters risk:** check the tester's expected `$ARGUMENTS` text. If a stage asserts
  collapsed whitespace, the tester would now see the raw spacing. Run the stage tests after.

### 0009 Closing frontmatter delimiter matched as a substring

- **Effort:** S
- **Location:** `internal/skills/skills.go:84` `bytes.Cut(content, sep)` with `sep = "---\n"`.
- **Plan:** replace line 84 with
  `header, body, found := bytes.Cut(append([]byte("\n"), content...), []byte("\n---\n"))`
  and a one-line comment saying why the `\n` is prepended (so an empty header still closes).
  Line 79 keeps `CutPrefix(content, []byte("---\n"))`.
- **Tests:** add three table cases to `TestInitSkillsRegistry` (`skills_test.go:45`): an inline
  `description: Separator ---`, a `# ---------` comment as the first header line, and a block
  scalar that contains an indented `---`.
- **Depends on:** none. 0010's final-newline append relies on this cut.

### 0010 Frontmatter rejected for CRLF, a UTF-8 BOM, or no final newline

- **Effort:** S
- **Location:** `internal/skills/skills.go:78-81` (exact `---\n` prefix, and the
  error literal at :81 contains a raw newline).
- **Plan:** at the top of `newSkill`:
  `content = bytes.ReplaceAll(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF")), []byte("\r\n"), []byte("\n"))`,
  then `if !bytes.HasSuffix(content, []byte("\n")) { content = append(content, '\n') }`.
  Change the message to ``"skill description not prefixed with `---`"``. Drop the `sep` variable.
- **Tests:** add CRLF, BOM and frontmatter-only-without-final-newline cases to `TestInitSkillsRegistry`.
  No existing test asserts the old error text.
- **Depends on:** 0009 (same lines, same commit).
- **Out of scope:** a leading blank line before `---` stays rejected, which is correct.

### 0011 Symlinked skill directories silently ignored

- **Effort:** S
- **Location:** `internal/skills/skills.go:115` `if !entry.IsDir()`.
- **Plan:** move `skillDir := filepath.Join(path, entry.Name())` (line 119) above the check
  and replace it with
  `if fi, statErr := os.Stat(skillDir); statErr != nil || !fi.IsDir() { continue }`.
  Dangling links are still skipped without a message.
- **Tests:** `TestInitSkillsRegistry_Symlinks`: `os.Symlink` a valid skill dir (loaded) and a
  dangling link (skipped).
- **Depends on:** none.

### 0012 Skill with no `name` registered under the empty key

- **Effort:** S
- **Location:** `internal/skills/skills.go:94` (no fallback) and `:134`
  (`skills[skill.Name]` with `""`).
- **Plan:** after Unmarshal (line 92): `s.Name = cmp.Or(s.Name, filepath.Base(path))`, and
  add the `cmp` import. This also removes the bare-`/` trigger.
- **Tests:** add a "skill without a name" table case: folder `deploy`, the registry key and
  `Name` are `deploy`.
- **Depends on:** none.

### 0017 Duplicate skill names silently overwrite each other

- **Effort:** S
- **Location:** `internal/skills/skills.go:134`. The later folder overwrites the
  earlier one, with no message.
- **Plan:** before line 134:
  `if prev, dup := skills[skill.Name]; dup { fmt.Fprintf(os.Stderr, "duplicate skill %q in %s, keeping %s\n", skill.Name, skillDir, prev.Path); continue }`.
  This keeps the first skill, matching the existing `Fprintf(os.Stderr, ...)` style.
  After 0029, use `logger.Warn("duplicate skill", ...)` instead.
- **Tests:** add a table case with `dupA` and `dupB` that both declare `name: dup`. The registry
  holds the dupA skill (compare `Description`). Asserting on stderr is optional, since there is
  no stderr-capture helper in the skills tests.
- **Depends on:** 0012 (two nameless skills now get distinct folder keys, not one duplicate).

## CLI entry point, flags, errors, logging (`app/main.go`)

Recommended order: 0004 and 0005 first, on stdlib `flag` (they do not need Cobra). After that, 0028 and then 0029 are optional.

### 0004 Every user-facing error is a Go panic with a stack trace and exit code 2

- **Effort:** S
- **Location:** `app/main.go:23` (`panic("Prompt must not be empty")`), `app/main.go:44`
  (completion error and Ctrl-C `context.Canceled`), `app/main.go:48` (stdout write), `internal/chat/client.go:23`
  (`panic("Env variable OPENROUTER_API_KEY not found")`), `app/main.go:18` (`flag.ExitOnError` makes a bad flag exit 2).
- **Plan:**
  - `internal/chat/client.go:21-30`: change it to `NewOpenAiClient() (openai.Client, error)` and return
    `errors.New("OPENROUTER_API_KEY is not set")` where it now panics. Keep the env var names (the CodeCrafters runner sets them).
  - `app/main.go`: change `newMainArgs` to return `(string, error)` and use `flag.ContinueOnError` with
    `fs.SetOutput(stderr)`. Return `errors.New("prompt must not be empty: use -p \"<prompt>\"")`.
  - Move the body of `main` into `run(ctx, args []string, stdout io.Writer) error`, so every error is returned and none panics.
  - Add `exitCode(err, stderr io.Writer) int`: it prints `app: <err>`, returns 130 on `errors.Is(err, context.Canceled)`,
    1 on any other error and 0 on nil. `main` then calls `os.Exit(exitCode(run(...), os.Stderr))`, after `stop()`,
    because `os.Exit` skips deferred calls.
  - To test the exit code, add a package var `exit = os.Exit`.
- **Tests:** `app/main_test.go` is `package main`, which is allowed for main. Replace the panic test at
  `app/main_test.go:19-30` with a table: missing `-p` → error, unknown flag `-x` → error, `-p hi` → "hi". Add tests for
  `exitCode(nil/plain/context.Canceled)` → 0/1/130 that check the stderr text. In `internal/chat`, test that
  `NewOpenAiClient` returns an error when the key is unset (`t.Setenv("OPENROUTER_API_KEY", "")`) and update its callers
  (24 call sites in tests across the repo use `InitSkillsRegistry`, `SendCompletion` or `NewOpenAiClient`, so grep before you edit).
- **Depends on:** none. 0005 touches the same function, so do both in one change.
- **CodeCrafters risk:** low. Success still exits 0 and prints only the answer to stdout.

### 0005 Unquoted prompt words after -p are silently dropped

- **Effort:** S
- **Location:** `app/main.go:20`. `fs.Parse` is called and `fs.Args()` is never checked, so
  `-p list all files` sends only `list`.
- **Plan:** in `newMainArgs`, after `fs.Parse`, add
  `if fs.NArg() > 0 { return "", fmt.Errorf("unexpected arguments %q: quote the prompt", fs.Args()) }`.
  This needs the error return from 0004.
- **Tests:** add table rows for `-p list all` → error that contains `quote the prompt`, and a positional `"hello"` → error.
- **Depends on:** 0004 (the error return instead of a panic).
- **Out of scope:** a positional prompt and `-p -` (stdin).
- **CodeCrafters risk:** low. The tester passes `-p "<prompt>"` as one argv element, and `your_program.sh`
  forwards `"$@"` quoted (`exec /tmp/codecrafters-build-claude-code-go "$@"`, last line).

### 0028 Migrate the CLI from stdlib flag to Cobra and Viper

- **Effort:** M
- **Location:** `app/main.go:5,18-20` (stdlib `flag`) and `internal/chat/client.go:22,27`
  (`os.Getenv` deep inside `chat`).
- **Plan:**
  - Add `NewRootCommand() *cobra.Command` with `Args: cobra.NoArgs` (or keep the 0005 message), a `-p/--prompt` string
    flag, `SilenceUsage`/`SilenceErrors`, and `RunE` that calls the `run` from 0004.
  - Use a local `v := viper.New()` (no globals), with `v.BindEnv("api_key", "OPENROUTER_API_KEY")` and
    `v.BindEnv("base_url", "OPENROUTER_BASE_URL")`, and a default base URL of `https://openrouter.ai/api/v1`.
  - Change it to `chat.NewOpenAiClient(apiKey, baseURL string) (openai.Client, error)` and remove the `os.Getenv` calls at `client.go:22,27`.
  - `main` becomes `exit(exitCode(NewRootCommand().ExecuteContext(ctx), os.Stderr))`.
  - go.mod adds `spf13/cobra` (v1.10.2) and `spf13/viper` (v1.21.0).
- **Tests:** drive `NewRootCommand()` with `SetArgs`, `SetOut` and `SetErr` against the existing `http.DefaultTransport`
  override in `app/main_test.go`. Cover the empty prompt, extra args, a missing key and the happy path. In `internal/chat`, test
  `NewOpenAiClient` with an empty key and an explicit baseURL. No `export_test.go` is needed in `app` (package main).
- **Depends on:** 0004 and 0005. Without them this is only a dependency swap.
- **Out of scope:** a config file and any flag beyond `-p`.
- **CodeCrafters risk:** medium. It adds two modules and their transitive dependencies to go.mod, and the remote build
  (`.codecrafters/compile.sh`, buildpack go-1.27) must be able to download them, or you must vendor them. Cobra accepts
  `-p x` as the shorthand for `--prompt`, so the invocation still works. Check that `-p` is defined with
  `StringP("prompt", "p", ...)`: a long-only flag would break the tester. **Decide whether to do this at all.** 0004 and 0005 fix
  the bugs with stdlib `flag`, and the challenge needs no extra flags.

### 0029 Switch diagnostics from fmt.Fprintf(os.Stderr) to log/slog

- **Effort:** S-M (S for the logger plumbing; M if a `--log-level` flag is added, because that needs 0028)
- **Location:** `internal/skills/skills.go:106,124,130` (`fmt.Fprintf(os.Stderr, ...)`) and
  `internal/chat/client.go:138` (`fmt.Fprintln(os.Stderr, res.Output)` on a failed tool).
- **Plan:**
  - Build `logger := slog.New(slog.NewTextHandler(os.Stderr, nil))` in `run` (default level Info, which still
    prints Warn and Error).
  - Change it to `skills.InitSkillsRegistry(logger *slog.Logger, path string)` and replace the 3 prints with
    `logger.Warn("skill not loaded", "path", ..., "err", ...)`.
  - Add a `logger` parameter to `chat.SendCompletion` (and its recursive fork call at `client.go:128`), and replace
    `client.go:138` with `logger.ErrorContext(ctx, "tool failed", "tool", tc.Function.Name, "err", execErr)`.
  - Do not use `slog.SetDefault` or a package-level logger.
- **Tests:** pass `slog.New(slog.NewTextHandler(&buf, nil))` into the skills and chat tests and assert that `buf`
  contains `msg="skill not loaded"` / `msg="tool failed"`. This replaces any stderr capture. Update all the call sites.
- **Depends on:** none, for a fixed level. A `--log-level` flag needs 0028.
- **CodeCrafters risk:** low. Logs still go only to stderr and stdout stays the final answer. The stderr text format
  changes (`level=WARN msg=...`), so check that no tester stage asserts on stderr.
