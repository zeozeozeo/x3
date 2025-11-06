package main

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"
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

	"github.com/zeozeozeo/x3/commands"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/modeled"
)

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
	slog.Info("disgo version", "version", disgo.Version)

	if err := db.InitDB(dbPath); err != nil {
		slog.Error("error while initializing database", "err", err)
		panic(err)
	}
	defer func() {
		if db.DB != nil {
			if err := db.DB.Close(); err != nil {
				slog.Error("error closing database", "err", err)
			} else {
				slog.Info("database connection closed")
			}
		}
	}()

	commands.StartTime = startTime

	r := handler.New()
	//r.Use(middleware.Logger)

	if err := commands.RegisterHandlers(r); err != nil {
		slog.Error("error while registering command handlers", "err", err)
		return
	}

	client, err := disgo.New(token,
		bot.WithShardManagerConfigOpts(
			sharding.WithShardIDs(0),
			sharding.WithShardCount(1),
			sharding.WithAutoScaling(true),
			sharding.WithGatewayConfigOpts(
				gateway.WithIntents(
					//gateway.IntentGuilds,
					gateway.IntentGuildMessages,
					gateway.IntentMessageContent,
					gateway.IntentDirectMessages,
					gateway.IntentGuildMessageReactions,
					gateway.IntentDirectMessageReactions,
				),
				gateway.WithCompress(true),
			),
		),
		bot.WithEventListeners(
			r,
			&events.ListenerAdapter{
				OnGuildReady: func(event *events.GuildReady) {
					slog.Info("guild ready", "id", event.GuildID)
				},
				OnGuildsReady: func(event *events.GuildsReady) {
					slog.Info("guilds on shard ready", "shard_id", event.ShardID())
				},
			},
		),
		bot.WithEventManagerConfigOpts(
			bot.WithAsyncEventsEnabled(),
		),
		bot.WithEventListenerFunc(commands.OnMessageCreate),
		bot.WithEventListenerFunc(commands.OnDMMessageReactionAdd),
		bot.WithEventListenerFunc(commands.OnGuildMessageReactionAdd),
	)
	if err != nil {
		slog.Error("error while building disgo instance", "err", err)
		return
	}
	defer client.Close(context.TODO())

	if _, err = client.Rest().SetGlobalCommands(client.ApplicationID(), commands.AllCommands); err != nil {
		panic(err)
	} else {
		slog.Info("global commands registered successfully")
	}

	//go func() {
	//	ticker := time.NewTicker(5 * time.Minute)
	//	defer ticker.Stop()
	//	for range ticker.C {
	//		commands.InitiateDMInteraction(client)
	//	}
	//}()

	// start narrator mainloop
	go commands.GetNarrator().Run()

	// start GUI server
	go func() {
		modelEditorServer := modeled.NewServer()
		if err := modelEditorServer.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("GUI server error", "err", err)
		}
	}()

	if err = client.OpenShardManager(context.TODO()); err != nil {
		slog.Error("error while connecting to gateway", "err", err)
		return
	}

	slog.Info("x3 is running. press ctrl+c to exit")
	slog.Info("GUI editor available at http://localhost" + modeled.Port)
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
	slog.Info("shutting down (SIGINT/SIGTERM)...")
}
