package persona

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// DiscordChannelRef is a compact, stable reference to a visible Discord channel.
type DiscordChannelRef struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}

// DiscordEnvironment captures Discord-specific metadata that should be stable for a channel.
type DiscordEnvironment struct {
	GuildName          string              `json:"guild_name,omitempty"`
	GuildDescription   string              `json:"guild_description,omitempty"`
	ChannelID          string              `json:"channel_id,omitempty"`
	ChannelName        string              `json:"channel_name,omitempty"`
	ChannelDescription string              `json:"channel_description,omitempty"`
	VisibleChannels    []DiscordChannelRef `json:"visible_channels,omitempty"`
}

func (env *DiscordEnvironment) IsEmpty() bool {
	return env == nil || (env.GuildName == "" &&
		env.GuildDescription == "" &&
		env.ChannelID == "" &&
		env.ChannelName == "" &&
		env.ChannelDescription == "" &&
		len(env.VisibleChannels) == 0)
}

// FluxerChannelRef is a compact, stable reference to a visible Fluxer channel.
type FluxerChannelRef struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}

// FluxerEnvironment captures Fluxer-specific metadata that should be stable for a channel.
type FluxerEnvironment struct {
	GuildName          string             `json:"guild_name,omitempty"`
	GuildDescription   string             `json:"guild_description,omitempty"`
	ChannelID          string             `json:"channel_id,omitempty"`
	ChannelName        string             `json:"channel_name,omitempty"`
	ChannelDescription string             `json:"channel_description,omitempty"`
	VisibleChannels    []FluxerChannelRef `json:"visible_channels,omitempty"`
}

func (env *FluxerEnvironment) IsEmpty() bool {
	return env == nil || (env.GuildName == "" &&
		env.GuildDescription == "" &&
		env.ChannelID == "" &&
		env.ChannelName == "" &&
		env.ChannelDescription == "" &&
		len(env.VisibleChannels) == 0)
}

// PromptContext is the stable, reusable context block that can be appended to a persona prompt.
type PromptContext struct {
	Summaries []Summary           `json:"summaries,omitempty"`
	Memories  []string            `json:"memories,omitempty"`
	Context   []string            `json:"context,omitempty"`
	Discord   *DiscordEnvironment `json:"discord,omitempty"`
	Fluxer    *FluxerEnvironment  `json:"fluxer,omitempty"`
}

func (ctx PromptContext) IsEmpty() bool {
	return ctx.Discord == nil && ctx.Fluxer == nil && len(ctx.Summaries) == 0 && len(ctx.Memories) == 0 && len(ctx.Context) == 0
}

// channelRef is the platform-neutral form of a channel reference used while
// rendering environment blocks.
type channelRef struct {
	id          string
	name        string
	category    string
	description string
}

// renderEnvironmentBlock renders the shared environment-block layout for a
// platform ("Discord" or "Fluxer").
func renderEnvironmentBlock(platform, guildName, guildDescription, channelID, channelName, channelDescription string, visible []channelRef) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s context:**\n", platform)
	if guildName != "" {
		fmt.Fprintf(&sb, "- Server: %s\n", html.UnescapeString(guildName))
	}
	if guildDescription != "" {
		fmt.Fprintf(&sb, "- Server description: %s\n", html.UnescapeString(guildDescription))
	}
	if channelName != "" {
		fmt.Fprintf(&sb, "- Current channel: #%s", html.UnescapeString(channelName))
		if channelID != "" {
			fmt.Fprintf(&sb, " (id: %s)", channelID)
		}
		sb.WriteRune('\n')
	}
	if channelDescription != "" {
		fmt.Fprintf(&sb, "- Current channel description: %s\n", html.UnescapeString(channelDescription))
	}
	if len(visible) > 0 {
		channels := append([]channelRef(nil), visible...)
		sort.SliceStable(channels, func(i, j int) bool {
			if channels[i].category != channels[j].category {
				return strings.ToLower(channels[i].category) < strings.ToLower(channels[j].category)
			}
			return strings.ToLower(channels[i].name) < strings.ToLower(channels[j].name)
		})
		sb.WriteString("- Accessible text channels:\n")
		for _, ch := range channels {
			chName := html.UnescapeString(strings.TrimSpace(ch.name))
			if chName == "" {
				continue
			}
			fmt.Fprintf(&sb, "  - #%s", chName)
			if id := strings.TrimSpace(ch.id); id != "" {
				fmt.Fprintf(&sb, " (id: %s)", id)
			}
			if cat := strings.TrimSpace(ch.category); cat != "" {
				fmt.Fprintf(&sb, " [%s]", html.UnescapeString(cat))
			}
			if desc := html.UnescapeString(strings.TrimSpace(ch.description)); desc != "" {
				fmt.Fprintf(&sb, " - %s", html.UnescapeString(desc))
			}
			sb.WriteRune('\n')
		}
	}
	return strings.TrimSpace(sb.String())
}

func (ctx PromptContext) BuildBlock() string {
	var sections []string

	if ctx.Discord != nil {
		refs := make([]channelRef, 0, len(ctx.Discord.VisibleChannels))
		for _, ch := range ctx.Discord.VisibleChannels {
			refs = append(refs, channelRef{id: ch.ID, name: ch.Name, category: ch.Category, description: ch.Description})
		}
		if block := renderEnvironmentBlock("Discord",
			ctx.Discord.GuildName, ctx.Discord.GuildDescription,
			ctx.Discord.ChannelID, ctx.Discord.ChannelName, ctx.Discord.ChannelDescription, refs); block != "" {
			sections = append(sections, block)
		}
	}

	if ctx.Fluxer != nil {
		refs := make([]channelRef, 0, len(ctx.Fluxer.VisibleChannels))
		for _, ch := range ctx.Fluxer.VisibleChannels {
			refs = append(refs, channelRef{id: ch.ID, name: ch.Name, category: ch.Category, description: ch.Description})
		}
		if block := renderEnvironmentBlock("Fluxer",
			ctx.Fluxer.GuildName, ctx.Fluxer.GuildDescription,
			ctx.Fluxer.ChannelID, ctx.Fluxer.ChannelName, ctx.Fluxer.ChannelDescription, refs); block != "" {
			sections = append(sections, block)
		}
	}

	if len(ctx.Summaries) > 0 {
		var sb strings.Builder
		sb.WriteString("**Past chat summaries:**\n")
		for _, summary := range ctx.Summaries {
			text := strings.TrimSpace(summary.Str)
			if text == "" {
				continue
			}
			fmt.Fprintf(&sb, "- %s\n", text)
		}
		block := strings.TrimSpace(sb.String())
		if block != "" {
			sections = append(sections, block)
		}
	}

	if len(ctx.Memories) > 0 {
		var sb strings.Builder
		sb.WriteString("**Chat memories:**\n")
		for _, item := range ctx.Memories {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			fmt.Fprintf(&sb, "- %s\n", text)
		}
		block := strings.TrimSpace(sb.String())
		if block != "" {
			sections = append(sections, block)
		}
	}

	if len(ctx.Context) > 0 {
		var sb strings.Builder
		sb.WriteString("**Additional instructions:**\n")
		for _, item := range ctx.Context {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			fmt.Fprintf(&sb, "- %s\n", text)
		}
		block := strings.TrimSpace(sb.String())
		if block != "" {
			sections = append(sections, block)
		}
	}

	return strings.Join(sections, "\n\n")
}
