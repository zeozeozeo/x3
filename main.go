package main

import (
	"context"
	// "database/sql" // Removed unused import
	_ "embed" // For sigmaBoyMp4
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	// load .env before importing our modules
	_ "github.com/joho/godotenv/autoload"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/sharding"

	"github.com/zeozeozeo/x3/commands" // Import the new commands package
	"github.com/zeozeozeo/x3/db"
)

//go:embed media/sigma-boy.mp4
var sigmaBoyMp4 []byte

var (
	token     = os.Getenv("X3_DISCORD_TOKEN")
	dbPath    = "x3.db"
	startTime = time.Now()
)

func levelFromString(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	slog.SetLogLoggerLevel(levelFromString(os.Getenv("X3_LOG_LEVEL")))
	slog.Info("x3 booting up...")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	// Initialize Database via commands package
	if err := db.InitDB(dbPath); err != nil {
		slog.Error("error while initializing database", slog.Any("err", err))
		panic(err) // Panic if DB init fails
	}
	// Close DB on exit
	defer func() {
		if db.DB != nil {
			if err := db.DB.Close(); err != nil {
				slog.Error("error closing database", slog.Any("err", err))
			} else {
				slog.Info("database connection closed")
			}
		}
	}()

	// Pass embedded data to commands package
	commands.SigmaBoyMp4 = sigmaBoyMp4
	// Pass start time to commands package
	commands.StartTime = startTime

	// Create command handler router
	r := handler.New()
	// r.Use(middleware.Logger)

	// Register all command handlers from the commands package
	if err := commands.RegisterHandlers(r); err != nil {
		slog.Error("error while registering command handlers", slog.Any("err", err))
		return
	}

	// Create bot client
	client, err := disgo.New(token,
		bot.WithShardManagerConfigOpts(
			sharding.WithShardIDs(0),
			sharding.WithShardCount(1),
			sharding.WithAutoScaling(true),
			sharding.WithGatewayConfigOpts(
				gateway.WithIntents(
					// gateway.IntentGuilds,
					gateway.IntentGuildMessages,
					gateway.IntentMessageContent,
					gateway.IntentDirectMessages,
				),
				gateway.WithCompress(true),
			),
		),
		bot.WithEventListeners(
			r, // Add the command handler router
			// Keep basic readiness listeners
			&events.ListenerAdapter{
				OnGuildReady: func(event *events.GuildReady) {
					slog.Info("guild ready", slog.Uint64("id", uint64(event.GuildID)))
				},
				OnGuildsReady: func(event *events.GuildsReady) {
					slog.Info("guilds on shard ready", slog.Uint64("shard_id", uint64(event.ShardID())))
				},
			},
		),
		bot.WithEventManagerConfigOpts(
			bot.WithAsyncEventsEnabled(),
		),
		// Register the message handler from the commands package
		bot.WithEventListenerFunc(commands.OnMessageCreate),
	)
	if err != nil {
		slog.Error("error while building disgo instance", slog.Any("err", err))
		return
	}
	defer client.Close(context.TODO())

	// Register slash commands with Discord
	if _, err = client.Rest().SetGlobalCommands(client.ApplicationID(), commands.GetAllCommandDefs()); err != nil {
		panic(err)
	} else {
		slog.Info("global commands registered successfully")
	}

	// Start the random DM interaction goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
		defer ticker.Stop()
		for range ticker.C {
			// Pass the client instance to the exported function
			commands.InitiateDMInteraction(client)
		}
	}()

	// Connect to gateway
	if err = client.OpenShardManager(context.TODO()); err != nil {
		slog.Error("error while connecting to gateway", slog.Any("err", err))
		return
	}

	slog.Info("x3 is running. press ctrl+c to exit")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
	slog.Info("shutting down...")
}
