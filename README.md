# x3

Peak Intelligence

A Discord LLM roleplay and utility bot

> [!NOTE]
> This is not meant to be selfhosted, please add the bot instead: https://discord.com/oauth2/authorize?client_id=1307635432632094740
>
> Or on Matrix (E2EE supported): [@x3_bot:matrix.org](https://matrix.to/#/@x3_bot:matrix.org)

## Features

- SillyTavern character cards `/persona card:url`
- Character card creator and editor `/personamaker new`, `/personamaker edit`
- Turn impersonate akin to SillyTavern `/impersonate`
- Server and DM quotes `/quote [...]`, `x3 quote`
- Image generation (powered by stablehorde) `/generate`
- Impromptu image generation with personas (akin to SillyTavern) `/persona images:true`
- Image editing utilities `x3 say`
- Additional Markov Chain, Eliza and ALICE algobots `/persona model:Markov Chain`
- Message regeneration & prefill `/regenerate`
- Per-channel image and text blacklist `/blacklist`
- In-chat context control (forget last n messages, all previous messages) `/lobotomy`
- Web-based model.json editor GUI
- Furry
- Works in DM, app and server contexts
- Extensive LLM model selection (GLM-5.1, DeepSeek V4, GPT-5, Llama, etcetc)
- Supports vision models, and automatically generates text descriptions for text-only models
- It has an extremely overcomplicated internal model router - it constantly juggles between vision and text models in a conversation (balancing between vision quality and response style consistency), in a very configurable manner
- It is optimized for the lowest running cost possible, aiming for maximum cache hits and routing to the cheapest providers
- Has a self-maintained toolcalling format that works even on providers that don't support the OpenAI one
- Aims to be compatible with every OpenAI API, even those with obscure nonstandard parameters (looking at you DeepSeek)
- Subpar $\LaTeX$ rendering
- Models can run websearches (grounding) and discord server searches
- `/antiscam` to patch up Discord's incompetent spammer problem (if you're a mod)
- Automatically preserves longer context history by summarizing previous messages
- Ability to export and import conversations `/chatlog export`, `/chatlog import`, `/lobotomy`
- Can render & embed HTML/SVG blocks (like SillyTavern's frontend does, but in Discord) with [Gotenberg](https://gotenberg.dev/)
- Free and Discord based, [add it](https://discord.com/oauth2/authorize?client_id=1307635432632094740)
- Also is a Matrix bot: [@x3_bot:matrix.org](https://matrix.to/#/@x3_bot:matrix.org)

## Matrix bot

Matrix support uses mautrix's pure-Go E2EE backend via the `goolm` build tag.

```console
go build -tags goolm -o x3
```

For all local terminal builds without passing `-tags` each time, set Go's user-level default:

```console
go env -w GOFLAGS=-tags=goolm
```

Set `X3_MATRIX_ENABLED=true` and the `X3_MATRIX_*` values from `.env.example`. If `X3_MATRIX_ACCESS_TOKEN` is empty, x3 logs in with `X3_MATRIX_USERNAME`/`X3_MATRIX_PASSWORD` and creates or reuses a dedicated Matrix device from `X3_MATRIX_CRYPTO_DB`. The Matrix UX uses text commands such as `!x3 persona`, `!x3 chat`, `!x3 context`, `!x3 lobotomy`, `!x3 regenerate`, and `!x3 chatlog export`.

For encrypted rooms, keep `X3_MATRIX_DEVICE_ID` and `X3_MATRIX_CRYPTO_DB` stable across restarts. To verify the headless bot device, set `X3_MATRIX_RECOVERY_KEY` to your Matrix recovery key before starting x3. The bot will import your cross-signing keys and sign its own device on startup.

> [!NOTE]
> Currently LaTeX rendering in Matrix is only supported by a couple clients; develop.element.io, SchildiChat and Nheko should work with the experimental feature enabled.

## Smart continuation triggers

x3 can optionally use [MiniLM](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2) locally to determine whether a message in a server is a continuation of the previous topic.

The ONNX model is loaded from disk but not included in the repo. Download a Sentence Transformers ONNX export, for example:

```console
cd models/minilm
curl -L -o models/minilm/all-MiniLM-L6-v2.onnx https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model_qint8_arm64.onnx
```

Set `X3_MINILM_MODEL_PATH` if you use another filename. ONNX Runtime must also be available as a shared library; set `X3_MINILM_ONNX_RUNTIME_LIB` or `ONNXRUNTIME_LIB_PATH` to its full path when it is not discoverable.

## Run in Docker (or Podman)

```console
podman build -t x3 -f Dockerfile .
podman run -d -v /path/to/your/containers/x3:/bot x3
```

To build a Discord-only container image without Matrix support:

```console
podman build --build-arg GO_BUILD_TAGS= -t x3 -f Dockerfile .
```
