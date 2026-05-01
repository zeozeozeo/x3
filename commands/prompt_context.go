package commands

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/persona"
)

type visibleChannelRef struct {
	ref              persona.DiscordChannelRef
	channelPosition  int
	categoryPosition int
}

func buildPromptContext(client *bot.Client, channelID snowflake.ID, guildID *snowflake.ID, cache *db.ChannelCache) persona.PromptContext {
	ctx := persona.PromptContext{
		Summaries: append([]persona.Summary(nil), cache.Summaries...),
		Context:   append([]string(nil), cache.Context...),
	}

	if guildID == nil {
		channel, err := client.Rest.GetChannel(channelID)
		if err == nil {
			if guildChannel, ok := channel.(discord.GuildChannel); ok {
				inferredGuildID := guildChannel.GuildID()
				guildID = &inferredGuildID
			}
		}
	}

	if guildID == nil {
		return ctx
	}

	env, err := buildDiscordEnvironment(client, channelID, *guildID)
	if err != nil {
		slog.Warn("failed to build discord environment for prompt context", "err", err, "channel_id", channelID.String(), "guild_id", guildID.String())
		if !env.IsEmpty() {
			ctx.Discord = env
		}
		return ctx
	}
	ctx.Discord = env
	return ctx
}

func buildDiscordEnvironment(client *bot.Client, channelID, guildID snowflake.ID) (*persona.DiscordEnvironment, error) {
	env := &persona.DiscordEnvironment{}

	channel, err := client.Rest.GetChannel(channelID)
	if err == nil && channel != nil {
		env.ChannelName, env.ChannelDescription = summarizeDiscordChannel(channel)
	}

	guild, err := client.Rest.GetGuild(guildID, false)
	if err != nil {
		return env, err
	}

	env.GuildName = strings.TrimSpace(guild.Name)
	if guild.Description != nil {
		env.GuildDescription = strings.TrimSpace(*guild.Description)
	}

	visible, err := buildVisibleChannelRefs(client, guild, channelID)
	if err != nil {
		slog.Warn("failed to enumerate visible channels", "err", err, "guild_id", guildID.String())
	} else {
		env.VisibleChannels = visible
	}

	return env, nil
}

func summarizeDiscordChannel(channel discord.Channel) (name, description string) {
	name = strings.TrimSpace(channel.Name())
	switch ch := channel.(type) {
	case discord.GuildMessageChannel:
		if topic := ch.Topic(); topic != nil {
			description = strings.TrimSpace(*topic)
		}
	case discord.GuildForumChannel:
		if ch.Topic != nil {
			description = strings.TrimSpace(*ch.Topic)
		}
	case discord.GuildMediaChannel:
		if ch.Topic != nil {
			description = strings.TrimSpace(*ch.Topic)
		}
	}
	return name, description
}

func buildVisibleChannelRefs(client *bot.Client, guild *discord.RestGuild, currentChannelID snowflake.ID) ([]persona.DiscordChannelRef, error) {
	guildChannels, err := client.Rest.GetGuildChannels(guild.ID)
	if err != nil {
		return nil, err
	}

	everyonePerms := everyoneRolePermissions(guild)
	if everyonePerms.Has(discord.PermissionAdministrator) {
		var refs []persona.DiscordChannelRef
		for _, channel := range guildChannels {
			if channel.ID() == currentChannelID {
				continue
			}
			if !isTextLikeChannel(channel) {
				continue
			}
			refs = append(refs, persona.DiscordChannelRef{
				Name:     strings.TrimSpace(channel.Name()),
				Category: strings.TrimSpace(categoryNameForChannel(guildChannels, channel.ParentID())),
			})
		}
		sortVisibleChannelRefs(refs)
		return refs, nil
	}

	categoryNameByID := map[snowflake.ID]string{}
	categoryPositionByID := map[snowflake.ID]int{}
	for _, channel := range guildChannels {
		if _, ok := channel.(discord.GuildCategoryChannel); !ok {
			continue
		}
		categoryNameByID[channel.ID()] = strings.TrimSpace(channel.Name())
		categoryPositionByID[channel.ID()] = channel.Position()
	}

	refs := make([]visibleChannelRef, 0, len(guildChannels))
	for _, channel := range guildChannels {
		if channel.ID() == currentChannelID || !isTextLikeChannel(channel) {
			continue
		}
		if !everyoneCanViewChannel(guild, channel) {
			continue
		}

		categoryName := ""
		categoryPos := 0
		if parentID := channel.ParentID(); parentID != nil {
			categoryName = categoryNameByID[*parentID]
			categoryPos = categoryPositionByID[*parentID]
		}

		refs = append(refs, visibleChannelRef{
			ref: persona.DiscordChannelRef{
				Name:     strings.TrimSpace(channel.Name()),
				Category: categoryName,
			},
			channelPosition:  channel.Position(),
			categoryPosition: categoryPos,
		})
	}

	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].categoryPosition != refs[j].categoryPosition {
			return refs[i].categoryPosition < refs[j].categoryPosition
		}
		if refs[i].ref.Category != refs[j].ref.Category {
			return strings.ToLower(refs[i].ref.Category) < strings.ToLower(refs[j].ref.Category)
		}
		if refs[i].channelPosition != refs[j].channelPosition {
			return refs[i].channelPosition < refs[j].channelPosition
		}
		return strings.ToLower(refs[i].ref.Name) < strings.ToLower(refs[j].ref.Name)
	})

	out := make([]persona.DiscordChannelRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.ref)
	}
	return out, nil
}

func sortVisibleChannelRefs(refs []persona.DiscordChannelRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Category != refs[j].Category {
			return strings.ToLower(refs[i].Category) < strings.ToLower(refs[j].Category)
		}
		return strings.ToLower(refs[i].Name) < strings.ToLower(refs[j].Name)
	})
}

func everyoneRolePermissions(guild *discord.RestGuild) discord.Permissions {
	for _, role := range guild.Roles {
		if role.ID == guild.ID {
			return role.Permissions
		}
	}
	return discord.PermissionsNone
}

func everyoneCanViewChannel(guild *discord.RestGuild, channel discord.GuildChannel) bool {
	perms := everyoneRolePermissions(guild)
	if perms.Has(discord.PermissionAdministrator) {
		return true
	}

	if overwrites := channel.PermissionOverwrites(); overwrites != nil {
		if ow, ok := overwrites.Role(guild.ID); ok {
			perms &^= ow.Deny
			perms |= ow.Allow
		}
	}

	return perms.Has(discord.PermissionViewChannel)
}

func isTextLikeChannel(channel discord.GuildChannel) bool {
	switch channel.(type) {
	case discord.GuildTextChannel, discord.GuildNewsChannel, discord.GuildForumChannel, discord.GuildMediaChannel:
		return true
	default:
		return false
	}
}

func categoryNameForChannel(channels []discord.GuildChannel, parentID *snowflake.ID) string {
	if parentID == nil {
		return ""
	}
	for _, channel := range channels {
		if channel.ID() != *parentID {
			continue
		}
		if _, ok := channel.(discord.GuildCategoryChannel); ok {
			return strings.TrimSpace(channel.Name())
		}
	}
	return ""
}
