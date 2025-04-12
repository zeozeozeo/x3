package commands

import (
	"fmt"
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/imagecmd" // Needed for image command definitions
	"github.com/zeozeozeo/x3/model"    // Needed for LLM command definitions

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// SigmaBoyMp4 holds the embedded video data, set by main.
var (
	SigmaBoyMp4 []byte

	AllCommands []discord.ApplicationCommandCreate = []discord.ApplicationCommandCreate{
		WhitelistCommand,
		LobotomyCommand,
		PersonaCommand,
		StatsCommand,
		QuoteCommand,
		RandomDMsCommand,
		RegenerateCommand,
		BlacklistCommand,
		MemoryCommand,
		ChatCommand, // generic /chat command
		GenerateCommand,
	}
)

func init() {
	// Add model-specific LLM commands
	AllCommands = append(AllCommands, GptCommands...)

	// Add image commands from the imagecmd package
	AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
		"boykisser", "Send boykisser image", []string{"Boykisser3", "girlkisser", "BoykisserHeaven", "TRANSKlSSER", "bothkisser", "boykisserADULT"}, updateInteractionError,
	))
	AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
		"furry", "Send a cute and relatable furry image", []string{"furry_irl"}, updateInteractionError,
	))
	AllCommands = append(AllCommands, imagecmd.MakeRedditImageCommand(
		"changed", "Send a meme about the game Changed", []string{"ChangedFurry"}, updateInteractionError,
	))
}

// RegisterHandlers registers all command and autocomplete handlers with the router.
func RegisterHandlers(r handler.Router) error {
	// Assert router type to *handler.Mux to access specific methods like NotFound
	mux, ok := r.(*handler.Mux)
	if !ok {
		return fmt.Errorf("RegisterHandlers requires a *handler.Mux, but received %T", r)
	}

	// LLM Commands
	// Register generic /chat handler
	mux.Command("/chat", func(e *handler.CommandEvent) error {
		return HandleLlm(e, nil) // Pass nil model for /chat
	})
	// Register model-specific handlers
	for _, m := range model.AllModels {
		// Create a new variable for the model in this loop iteration
		// to avoid capturing the loop variable in the closure.
		currentModel := m
		if currentModel.Command != "chat" { // Don't re-register /chat
			mux.Command("/"+currentModel.Command, func(event *handler.CommandEvent) error {
				// Whitelist check is now inside HandleLlm
				return HandleLlm(event, &currentModel)
			})
		}
	}

	// Other Commands
	mux.Command("/whitelist", HandleWhitelist)
	mux.Command("/lobotomy", HandleLobotomy)
	mux.Command("/persona", HandlePersona)
	mux.Autocomplete("/persona", HandlePersonaModelAutocomplete) // Autocomplete for /persona model option
	mux.Command("/stats", HandleStats)
	mux.Command("/random_dms", HandleRandomDMs)
	mux.Command("/regenerate", HandleRegenerate)
	mux.Command("/blacklist", HandleBlacklist)

	// Quote Subcommands & Autocomplete
	mux.Route("/quote", func(r handler.Router) {
		// Assert this sub-router as well if needed, though likely not necessary
		// if only using Command/Autocomplete methods.
		r.Use() // Middleware placeholder if needed in future
		r.Autocomplete("/get", HandleQuoteGetAutocomplete)
		r.Command("/get", HandleQuoteGet)
		r.Command("/random", HandleQuoteRandom)
		r.Command("/new", HandleQuoteNew)
		r.Autocomplete("/remove", HandleQuoteGetAutocomplete) // Use same autocomplete for remove
		r.Command("/remove", HandleQuoteRemove)
	})

	// Memory Subcommands
	mux.Route("/memory", func(r handler.Router) {
		r.Use()
		r.Command("/list", HandleMemoryList)
		r.Command("/clear", HandleMemoryClear)
		r.Autocomplete("/delete", HandleMemoryDeleteAutocomplete)
		r.Command("/delete", HandleMemoryDelete)
		r.Command("/add", HandleMemoryAdd)
	})

	r.Autocomplete("/generate", HandleGenerateModelAutocomplete)
	r.Command("/generate", HandleGenerate)
	r.ButtonComponent("/cancel/{id}", HandleGenerateCancel)

	// Image Commands (Registered via imagecmd package)
	// Pass the asserted *handler.Mux to imagecmd
	imagecmd.RegisterCommands(mux)

	// Not Found Handler (using the asserted mux)
	mux.NotFound(HandleNotFound)

	slog.Info("command handlers registered")
	return nil
}
