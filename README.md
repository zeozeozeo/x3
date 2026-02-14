# x3

Peak Intelligence

A Discord utility bot with LLM intergration

> [!NOTE]
> This is not meant to be selfhosted, please add the bot instead: https://discord.com/oauth2/authorize?client_id=1307635432632094740

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
- Extensive LLM model selection (GLM-5, Minimax, DeepSeek V3.2, GPT-5, Llama, Kimi K2.5, etcetc)
- Supports vision models
- Subpar $\LaTeX$ rendering
- Free and Discord based, [add it](https://discord.com/oauth2/authorize?client_id=1307635432632094740)

## Run in Docker (or Podman)

```console
podman build -t x3 -f Dockerfile .
podman run -d -v /path/to/your/containers/x3:/bot x3
```
