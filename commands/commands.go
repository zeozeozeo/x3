package commands

import (
	"fmt"
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/model"

	_ "github.com/mattn/go-sqlite3"
)

var AllCommands []discord.ApplicationCommandCreate = []discord.ApplicationCommandCreate{
	WhitelistCommand,
	LobotomyCommand,
	PersonaCommand,
	StatsCommand,
	QuoteCommand,
	RandomDMsCommand,
	RegenerateCommand,
	BlacklistCommand,
	ChatCommand, // generic /chat command
	GenerateCommand,
	ImpersonateCommand,
	contextCommand,
	AntiscamCommand,
}

func init() {
	AllCommands = append(AllCommands, GptCommands...)

	//AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
	//	"boykisser", "Send boykisser image", []string{"Boykisser3", "girlkisser", "BoykisserHeaven", "TRANSKlSSER", "bothkisser", "boykisserADULT"}, updateInteractionError,
	//))
	//AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
	//	"furry", "Send a cute and relatable furry image", []string{"furry_irl"}, updateInteractionError,
	//))
	//AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
	//	"changed", "Send a meme about the game Changed", []string{"ChangedFurry"}, updateInteractionError,
	//))
}

// RegisterHandlers registers all command and autocomplete handlers with the router.
func RegisterHandlers(r handler.Router) error {
	mux, ok := r.(*handler.Mux)
	if !ok {
		return fmt.Errorf("RegisterHandlers requires a *handler.Mux, but received %T", r)
	}

	// llm commands
	mux.Command("/chat", func(e *handler.CommandEvent) error {
		return HandleLlm(e, nil)
	})
	for _, m := range model.AllModels {
		currentModel := m
		if currentModel.Command != "chat" {
			mux.Command("/"+currentModel.Command, func(event *handler.CommandEvent) error {
				return HandleLlm(event, []model.Model{currentModel})
			})
		}
	}

	// other commands
	mux.Command("/whitelist", HandleWhitelist)
	mux.Command("/lobotomy", HandleLobotomy)
	mux.Command("/persona", HandlePersona)
	mux.Autocomplete("/persona", HandlePersonaModelAutocomplete)
	mux.Command("/stats", HandleStats)
	mux.Command("/random_dms", HandleRandomDMs)
	mux.Command("/regenerate", HandleRegenerate)
	mux.Command("/blacklist", HandleBlacklist)

	// /quote
	mux.Route("/quote", func(r handler.Router) {
		r.Use()
		r.Autocomplete("/get", HandleQuoteGetAutocomplete)
		r.Command("/get", HandleQuoteGet)
		r.Command("/random", HandleQuoteRandom)
		r.Command("/new", HandleQuoteNew)
		r.Autocomplete("/remove", HandleQuoteGetAutocomplete)
		r.Command("/remove", HandleQuoteRemove)
	})

	mux.Autocomplete("/generate", HandleGenerateModelAutocomplete)
	mux.Command("/generate", HandleGenerate)
	mux.ButtonComponent("/cancel/{id}", HandleGenerateCancel)

	mux.Command("/impersonate", HandleImpersonate)
	mux.Command("/context", handleContext)
	mux.Command("/antiscam", HandleAntiscam)

	// image commands
	//imagecmd.RegisterCommands(mux)

	mux.NotFound(HandleNotFound)

	slog.Info("command handlers registered")
	return nil
}
