package persona

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// DiscordChannelRef is a compact, stable reference to a visible Discord channel.
type DiscordChannelRef struct {
	Name        string `json:"name,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}

// DiscordEnvironment captures Discord-specific metadata that should be stable for a channel.
type DiscordEnvironment struct {
	GuildName          string              `json:"guild_name,omitempty"`
	GuildDescription   string              `json:"guild_description,omitempty"`
	ChannelName        string              `json:"channel_name,omitempty"`
	ChannelDescription string              `json:"channel_description,omitempty"`
	VisibleChannels    []DiscordChannelRef `json:"visible_channels,omitempty"`
}

func (env *DiscordEnvironment) IsEmpty() bool {
	return env == nil || (env.GuildName == "" &&
		env.GuildDescription == "" &&
		env.ChannelName == "" &&
		env.ChannelDescription == "" &&
		len(env.VisibleChannels) == 0)
}

// PromptContext is the stable, reusable context block that can be appended to a persona prompt.
type PromptContext struct {
	Summaries []Summary           `json:"summaries,omitempty"`
	Context   []string            `json:"context,omitempty"`
	Discord   *DiscordEnvironment `json:"discord,omitempty"`
}

func (ctx PromptContext) IsEmpty() bool {
	return ctx.Discord == nil && len(ctx.Summaries) == 0 && len(ctx.Context) == 0
}

func (ctx PromptContext) BuildBlock() string {
	var sections []string

	if ctx.Discord != nil {
		var sb strings.Builder
		sb.WriteString("**Discord context:**\n")
		if ctx.Discord.GuildName != "" {
			fmt.Fprintf(&sb, "- Server: %s\n", html.UnescapeString(ctx.Discord.GuildName))
		}
		if ctx.Discord.GuildDescription != "" {
			fmt.Fprintf(&sb, "- Server description: %s\n", html.UnescapeString(ctx.Discord.GuildDescription))
		}
		if ctx.Discord.ChannelName != "" {
			fmt.Fprintf(&sb, "- Current channel: #%s\n", html.UnescapeString(ctx.Discord.ChannelName))
		}
		if ctx.Discord.ChannelDescription != "" {
			fmt.Fprintf(&sb, "- Current channel description: %s\n", html.UnescapeString(ctx.Discord.ChannelDescription))
		}
		if len(ctx.Discord.VisibleChannels) > 0 {
			channels := clone(ctx.Discord.VisibleChannels)
			sort.SliceStable(channels, func(i, j int) bool {
				if channels[i].Category != channels[j].Category {
					return strings.ToLower(channels[i].Category) < strings.ToLower(channels[j].Category)
				}
				return strings.ToLower(channels[i].Name) < strings.ToLower(channels[j].Name)
			})
			sb.WriteString("- Accessible text channels:\n")
			for _, ch := range channels {
				chName := html.UnescapeString(strings.TrimSpace(ch.Name))
				if chName == "" {
					continue
				}
				fmt.Fprintf(&sb, "  - #%s", chName)
				if cat := strings.TrimSpace(ch.Category); cat != "" {
					fmt.Fprintf(&sb, " [%s]", html.UnescapeString(cat))
				}
				if desc := html.UnescapeString(strings.TrimSpace(ch.Description)); desc != "" {
					fmt.Fprintf(&sb, " - %s", desc)
				}
				sb.WriteRune('\n')
			}
		}
		sections = append(sections, strings.TrimSpace(sb.String()))
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
