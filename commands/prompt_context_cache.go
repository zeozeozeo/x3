package commands

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/persona"
)

const discordEnvironmentCacheTTL = 12 * time.Hour

type cachedDiscordEnvironment struct {
	guildID   snowflake.ID
	updatedAt time.Time
	env       persona.DiscordEnvironment
}

var discordEnvironmentCache sync.Map // snowflake.ID(channel) -> cachedDiscordEnvironment

func InvalidateDiscordEnvironmentCache(channelID snowflake.ID) {
	discordEnvironmentCache.Delete(channelID)
}

func InvalidateDiscordEnvironmentCacheByGuild(guildID snowflake.ID) {
	discordEnvironmentCache.Range(func(key, value any) bool {
		entry, ok := value.(cachedDiscordEnvironment)
		if !ok {
			discordEnvironmentCache.Delete(key)
			return true
		}
		if entry.guildID == guildID {
			discordEnvironmentCache.Delete(key)
		}
		return true
	})
}

func getDiscordEnvironmentSnapshot(client *bot.Client, channelID snowflake.ID, guildID *snowflake.ID) (*persona.DiscordEnvironment, error) {
	if cached, ok := discordEnvironmentCache.Load(channelID); ok {
		if entry, ok := cached.(cachedDiscordEnvironment); ok {
			if time.Since(entry.updatedAt) <= discordEnvironmentCacheTTL {
				if guildID == nil || entry.guildID == 0 || entry.guildID == *guildID {
					env := entry.env
					return &env, nil
				}
			}
		}
	}

	env, resolvedGuildID, err := buildDiscordEnvironmentFresh(client, channelID, guildID)
	if err != nil {
		return env, err
	}
	if resolvedGuildID != 0 {
		discordEnvironmentCache.Store(channelID, cachedDiscordEnvironment{
			guildID:   resolvedGuildID,
			updatedAt: time.Now(),
			env:       *env,
		})
	}
	return env, nil
}

type discordGuildSnapshot struct {
	ID          snowflake.ID
	Name        string
	Description *string
	Roles       []discord.Role
}

func buildDiscordEnvironmentFresh(client *bot.Client, channelID snowflake.ID, guildID *snowflake.ID) (*persona.DiscordEnvironment, snowflake.ID, error) {
	env := &persona.DiscordEnvironment{}

	channel, resolvedGuildID, err := getDiscordChannelSnapshot(client, channelID)
	if err == nil && channel != nil {
		env.ChannelName, env.ChannelDescription = summarizeDiscordChannel(channel)
		if guildID == nil && resolvedGuildID != 0 {
			guildID = &resolvedGuildID
		}
	}

	if guildID == nil {
		return env, 0, nil
	}

	guild, err := getDiscordGuildSnapshot(client, *guildID)
	if err != nil {
		return env, *guildID, err
	}

	env.GuildName = strings.TrimSpace(guild.Name)
	if guild.Description != nil {
		env.GuildDescription = strings.TrimSpace(*guild.Description)
	}

	visible, err := buildVisibleChannelRefs(client, guild, channelID)
	if err != nil {
		slog.Warn("failed to enumerate visible channels", "err", err, "guild_id", guild.ID.String())
	} else {
		env.VisibleChannels = visible
	}

	return env, guild.ID, nil
}

func getDiscordChannelSnapshot(client *bot.Client, channelID snowflake.ID) (discord.GuildChannel, snowflake.ID, error) {
	if cached, ok := client.Caches.Channel(channelID); ok {
		if cached == nil {
			return nil, 0, fmt.Errorf("channel cache entry was nil")
		}
		if guildChannel, ok := cached.(discord.GuildChannel); ok {
			guildID := guildChannel.GuildID()
			return guildChannel, guildID, nil
		}
	}

	channel, err := client.Rest.GetChannel(channelID)
	if err != nil {
		return nil, 0, err
	}
	guildChannel, ok := channel.(discord.GuildChannel)
	if !ok {
		return nil, 0, fmt.Errorf("channel %s is not a guild channel", channelID.String())
	}
	guildID := guildChannel.GuildID()
	return guildChannel, guildID, nil
}

func getDiscordGuildSnapshot(client *bot.Client, guildID snowflake.ID) (*discordGuildSnapshot, error) {
	if cached, ok := client.Caches.Guild(guildID); ok {
		return &discordGuildSnapshot{
			ID:          cached.ID,
			Name:        cached.Name,
			Description: cached.Description,
			Roles:       collectCachedRoles(client, guildID),
		}, nil
	}

	guild, err := client.Rest.GetGuild(guildID, false)
	if err != nil {
		return nil, err
	}
	return &discordGuildSnapshot{
		ID:          guild.ID,
		Name:        guild.Name,
		Description: guild.Description,
		Roles:       guild.Roles,
	}, nil
}

func collectCachedRoles(client *bot.Client, guildID snowflake.ID) []discord.Role {
	roles := make([]discord.Role, 0)
	for role := range client.Caches.Roles(guildID) {
		roles = append(roles, role)
	}
	return roles
}

func getGuildChannelsSnapshot(client *bot.Client, guildID snowflake.ID) ([]discord.GuildChannel, error) {
	var channels []discord.GuildChannel
	for channel := range client.Caches.ChannelsForGuild(guildID) {
		channels = append(channels, channel)
	}
	if len(channels) > 0 {
		return channels, nil
	}

	restChannels, err := client.Rest.GetGuildChannels(guildID)
	if err != nil {
		return nil, err
	}
	channels = make([]discord.GuildChannel, 0, len(restChannels))
	for _, channel := range restChannels {
		channels = append(channels, channel)
	}
	return channels, nil
}
