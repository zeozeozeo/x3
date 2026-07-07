package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/persona"
	"github.com/zeozeozeo/x3/site"
)

var SiteCommand = discord.SlashCommandCreate{
	Name:        "site",
	Description: "Generate a private dynamic website with an LLM",
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
		discord.ApplicationCommandOptionString{
			Name:        "theme",
			Description: "What the infinite site should be about",
			Required:    true,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "include_persona",
			Description: "Include the active persona system prompt instead of only /context",
			Required:    false,
		},
	},
}

var siteManager *site.Manager

func SetSiteManager(manager *site.Manager) {
	siteManager = manager
}

func HandleSite(event *handler.CommandEvent) error {
	if siteManager == nil {
		return sendInteractionError(event, "site runtime is not available", true)
	}
	if err := event.DeferCreateMessage(false); err != nil {
		return err
	}
	theme := event.SlashCommandInteractionData().String("theme")
	includePersona, _ := event.SlashCommandInteractionData().OptBool("include_persona")
	cache := db.GetChannelCache(event.Channel().ID())
	additionalContext := append([]string(nil), cache.Context...)
	var personaSystem string
	if includePersona {
		personaSystem = persona.GetPersonaByMeta(
			cache.PersonaMeta,
			event.User().EffectiveName(),
			event.GuildID() == nil,
			persona.PromptContext{Context: append([]string(nil), cache.Context...)},
		).System
		additionalContext = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := siteManager.CreateSite(ctx, site.CreateOptions{
		CreatorID:         event.User().ID.String(),
		Theme:             theme,
		AdditionalContext: additionalContext,
		PersonaSystem:     personaSystem,
	})
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	content := fmt.Sprintf(
		"Live site: %s\n-# Deleted automatically <t:%d:R>",
		result.URL,
		result.ExpiresAt.Unix(),
	)
	msg, err := event.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			WithContent(content).
			AddActionRow(
				discord.ButtonComponent{
					Style:    discord.ButtonStyleSecondary,
					Label:    "Cancel Site",
					Emoji:    &discord.ComponentEmoji{Name: "❌"},
					CustomID: fmt.Sprintf("/sitecancel/%s:%s", result.SiteID, event.User().ID.String()),
				},
			),
	)
	if err != nil {
		return err
	}
	return siteManager.AttachDiscordMessage(result.SiteID, msg.ID.String())
}

func HandleSiteCancel(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	if siteManager == nil {
		return event.CreateMessage(discord.NewMessageCreate().WithContent("Site runtime is not available").WithEphemeral(true))
	}
	_, customID, ok := strings.Cut(data.CustomID()[1:], "/")
	if !ok {
		return fmt.Errorf("invalid custom id: %s", data.CustomID())
	}
	siteID, userID, ok := strings.Cut(customID, ":")
	if !ok {
		return fmt.Errorf("invalid custom id: %s", data.CustomID())
	}
	if userID != event.User().ID.String() {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("Cannot cancel another user's site").
				WithEphemeral(true),
		)
	}
	if err := event.DeferUpdateMessage(); err != nil {
		return err
	}
	if err := siteManager.CancelSite(siteID, event.User().ID.String()); err != nil {
		_, updateErr := event.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent(err.Error()))
		if updateErr != nil {
			return updateErr
		}
		return err
	}
	if err := event.Client().Rest.DeleteMessage(event.Channel().ID(), event.Message.ID); err == nil {
		return nil
	}
	return event.UpdateMessage(
		discord.NewMessageUpdate().
			WithContent("Site canceled and deleted.").
			ClearComponents(),
	)
}
