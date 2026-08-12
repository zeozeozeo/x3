# Repository Guidelines

## Project Structure & Module Organization

This is a Go Discord and Matrix bot. The executable entrypoint is `main.go`; auxiliary commands live in `cmd/`. Core bot behavior is in `commands/`, language-model routing in `llm/` and `model/`, persona and character-card logic in `persona/`, and SQLite persistence in `db/`. The `modeled/` package contains the embedded model-editor server, HTML templates, JavaScript, and CSS. Provider/API integrations are grouped in packages such as `openai/`, `horder/`, `ddg/`, and `site/`. Tests are colocated with implementation files using `_test.go`.

## Build, Test, and Development Commands

Run the standard test suite with:

```console
go test ./...
```

Run focused tests while iterating:

```console
go test ./commands ./db ./modeled ./model
```

Format changed Go files with `gofmt -w path/to/file.go`; validate packages with `go vet ./...`. A normal local build is `go build -o x3.exe .`. Matrix builds require the `goolm` tag, as documented in `README.md`: `go build -tags goolm -o x3 .`.

## Coding Style & Naming Conventions

Use standard `gofmt` formatting, tabs for Go indentation, and idiomatic Go names: exported identifiers use PascalCase and unexported helpers use camelCase. Keep package responsibilities focused and reuse existing helpers for Discord messages, response splitting, database access, and model loading. Frontend files under `modeled/static/` use two-space JavaScript/CSS indentation and descriptive kebab-case DOM IDs.

## Context and Message Handling

The normal chat path is `handleLlmInteraction2` in `commands/llm_interact.go`. It starts with the channel cache, then uses imported history when present, the cached `llm.Llmer` when available, or `addContextMessages` to load Discord messages before the triggering message. `addContextMessages` distinguishes user and bot messages, restores rendered content, preserves split continuations, and skips control, card, chatlog, narration, and other non-conversational messages. Changes that add messages or context must follow these existing filters.

Before changing prompt or message handling, inspect both prefix and suffix conventions: persona `Prepend` is an assistant prefill, split markers and zero-width continuation markers affect history reconstruction, and `getMessageContent`/`formatMsg` normalize incoming text. Do not feed button prompts, status messages, errors, generated-image narration, comparison UI, or command-only messages into the LLM. Add a classifier or explicit skip condition for any new non-LLM message type, and verify both live Discord history and cached/imported history paths.

## Testing Guidelines

Use Go’s built-in `testing` package. Name tests `TestThing` and table-driven cases where multiple inputs exercise one behavior. Add regression tests beside the package being changed, especially for persistence, message formatting, model selection, and Discord interaction state. Run focused tests first, then `go test ./...` before submitting.

## Security & Configuration

Use `.env.example` as the configuration reference and keep credentials in `.env` or the deployment environment. Treat `x3.db`, model-provider settings, Discord tokens, Matrix credentials, and external service URLs as sensitive. Preserve SQLite migrations and test database behavior when changing `db/`.
