package imagecmd

import (
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/reddit"
)

const maxRedditAttempts = 5

type imageHandler struct {
	handle     func(event *handler.CommandEvent) error
	refresh    func(data discord.ButtonInteractionData, event *handler.ComponentEvent) error
	refreshID  string
	name       string // name without '/'
	subreddits []string
}

var handlers map[string]imageHandler = make(map[string]imageHandler) // maps refreshID to imageHandler

func redditFetchImage(subreddits []string, attempts int) (reddit.Post, error) {
	slog.Info("redditFetchImage", slog.Int("attempts", attempts), slog.Any("subreddits", subreddits))

	post, err := reddit.GetRandomImageFromSubreddits(subreddits...)
	if err != nil {
		if attempts < maxRedditAttempts {
			return redditFetchImage(subreddits, attempts+1)
		}
		return post, err
	}

	url := post.Data.GetRandomImage()

	/*
		// silly discord thing: we can't make image attachments using the URL;
		// we actually have to fetch the file and upload it as an octet stream
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			if attempts < maxRedditAttempts {
				return redditFetchImage(subreddits, attempts+1)
			}
			return nil, post, err
		}
		req.Header.Set("User-Agent", reddit.UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			if attempts < maxRedditAttempts {
				return redditFetchImage(subreddits, attempts+1)
			}
			return nil, post, err
		}
	*/

	slog.Info("redditFetchImage: got post", slog.String("url", url), slog.Any("subreddits", subreddits))

	return post, nil
}

func MakeRedditImageCommand(
	name, desc string,
	subreddits []string,
	errorHandler func(event *handler.CommandEvent, msg string) error,
) discord.SlashCommandCreate {
	hnd := imageHandler{
		handle: func(event *handler.CommandEvent) error {
			data := event.SlashCommandInteractionData()
			ephemeral := data.Bool("ephemeral")

			event.DeferCreateMessage(ephemeral)

			post, err := redditFetchImage(subreddits, 1)
			if err != nil {
				if errorHandler != nil {
					return errorHandler(event, err.Error())
				}
				return nil
			}

			var flags discord.MessageFlags
			if ephemeral {
				flags = discord.MessageFlagEphemeral
			}

			url := post.Data.GetRandomImage()
			_, err = event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
				SetContent(url).
				AddContainerComponents(
					discord.ActionRowComponent{
						discord.ButtonComponent{
							Style: discord.ButtonStyleLink,
							Emoji: &discord.ComponentEmoji{
								Name: "💦",
							},
							URL: post.Data.GetPostLink(),
						},
						discord.ButtonComponent{
							Style: discord.ButtonStyleSecondary,
							Emoji: &discord.ComponentEmoji{
								Name: "🔄",
							},
							CustomID: "refresh_" + name,
						},
					},
				).
				SetFlags(flags).
				Build())

			return err
		},
		refresh: func(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
			event.DeferUpdateMessage()
			post, err := redditFetchImage(handlers[data.CustomID()].subreddits, 1)
			if err != nil {
				return err
			}
			url := post.Data.GetRandomImage()
			_, err = event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
				SetContent(url).
				AddContainerComponents(
					discord.ActionRowComponent{
						discord.ButtonComponent{
							Style: discord.ButtonStyleLink,
							Emoji: &discord.ComponentEmoji{
								Name: "💦",
							},
							URL: post.Data.GetPostLink(),
						},
						discord.ButtonComponent{
							Style: discord.ButtonStyleSecondary,
							Emoji: &discord.ComponentEmoji{
								Name: "🔄",
							},
							CustomID: data.CustomID(),
						},
					},
				).
				Build())
			return err
		},
		refreshID:  "refresh_" + name,
		name:       name,
		subreddits: subreddits,
	}

	handlers["refresh_"+name] = hnd

	return discord.SlashCommandCreate{
		Name:        name,
		Description: desc,
		IntegrationTypes: []discord.ApplicationIntegrationType{
			discord.ApplicationIntegrationTypeGuildInstall,
			discord.ApplicationIntegrationTypeUserInstall,
		},
		Contexts: []discord.InteractionContextType{
			discord.InteractionContextTypeGuild,
			discord.InteractionContextTypeBotDM,
			discord.InteractionContextTypePrivateChannel,
		},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionBool{
				Name:        "ephemeral",
				Description: "If the response should only be visible to you",
				Required:    false,
			},
		},
	}
}

func RegisterCommands(r *handler.Mux) {
	for _, handler := range handlers {
		slog.Info("Registering command", slog.String("name", handler.name))
		r.Command("/"+handler.name, handler.handle)
		r.ButtonComponent("/"+handler.refreshID, handler.refresh)
	}
}
