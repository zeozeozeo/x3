package commands

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
)

var (
	discordFromFilterRegexp   = regexp.MustCompile(`(?i)\bfrom:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordInFilterRegexp     = regexp.MustCompile(`(?i)\bin:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordHasFilterRegexp    = regexp.MustCompile(`(?i)\bhas:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordMentionsRegexp     = regexp.MustCompile(`(?i)\bmentions:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordBeforeFilterRegexp = regexp.MustCompile(`(?i)\bbefore:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordAfterFilterRegexp  = regexp.MustCompile(`(?i)\bafter:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
	discordPinnedFilterRegexp = regexp.MustCompile(`(?i)\bpinned:(?:"([^"]+)"|'([^']+)'|([^\s]+))`)
)

type discordSearchSpec struct {
	Content  string
	From     []string
	In       []string
	Has      []discord.MessageSearchHasType
	Mentions []string
	MaxID    snowflake.ID
	MinID    snowflake.ID
	Pinned   *bool
}

func configureDiscordSearchTool(llmer *llm.Llmer, client *bot.Client, guildID *snowflake.ID, requesterID snowflake.ID, requesterName string, includeNSFW bool) {
	llmer.SetGuildID(guildID)
	if guildID == nil {
		llmer.SetDiscordSearchCallback(nil)
		return
	}

	llmer.SetDiscordSearchCallback(func(ctx context.Context, guildID snowflake.ID, query string) (string, map[int]string) {
		return getDiscordSearchResults(ctx, client, guildID, requesterID, requesterName, query, includeNSFW)
	})
}

func getDiscordSearchResults(ctx context.Context, client *bot.Client, guildID snowflake.ID, requesterID snowflake.ID, requesterName, query string, includeNSFW bool) (string, map[int]string) {
	citemap := make(map[int]string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "<discord search query was empty>", citemap
	}

	spec := parseDiscordSearchSpec(query)
	authorIDs, unresolvedAuthorFilters := resolveDiscordAuthorIDs(client, guildID, requesterID, requesterName, spec.From)
	channelIDs, unresolvedChannelFilters := resolveDiscordChannelIDs(client, guildID, spec.In)
	mentionIDs, unresolvedMentionFilters := resolveDiscordUserIDs(client, guildID, requesterID, requesterName, spec.Mentions)

	if len(spec.From) > 0 && len(authorIDs) == 0 {
		return fmt.Sprintf("<could not resolve author filter(s): %s>", strings.Join(unresolvedAuthorFilters, ", ")), citemap
	}
	if len(spec.In) > 0 && len(channelIDs) == 0 {
		return fmt.Sprintf("<could not resolve channel filter(s): %s>", strings.Join(unresolvedChannelFilters, ", ")), citemap
	}
	if len(spec.Mentions) > 0 && len(mentionIDs) == 0 {
		return fmt.Sprintf("<could not resolve mention filter(s): %s>", strings.Join(unresolvedMentionFilters, ", ")), citemap
	}

	channelNames := map[snowflake.ID]string{}
	if channels, err := client.Rest.GetGuildChannels(guildID); err == nil {
		for _, ch := range channels {
			name := strings.TrimSpace(ch.Name())
			if name == "" {
				continue
			}
			channelNames[ch.ID()] = name
		}
	} else {
		slog.Warn("discord search: failed to fetch guild channels", "err", err, "guild_id", guildID.String())
	}

	search := discord.GuildMessagesSearch{
		Limit:       8,
		Content:     spec.Content,
		AuthorIDs:   authorIDs,
		ChannelIDs:  channelIDs,
		Mentions:    mentionIDs,
		Has:         spec.Has,
		MaxID:       spec.MaxID,
		MinID:       spec.MinID,
		Pinned:      spec.Pinned,
		SortBy:      discord.MessageSearchSortByRelevance,
		SortOrder:   discord.MessageSearchSortOrderDesc,
		IncludeNSFW: &includeNSFW,
	}

	result, err := client.Rest.SearchGuildMessages(ctx, guildID, search)
	if err != nil {
		return fmt.Sprintf("<failed to search Discord messages for '%s': %v>", spec.Content, err), citemap
	}

	for _, thread := range result.Threads {
		name := strings.TrimSpace(thread.Name())
		if name != "" {
			channelNames[thread.ID()] = name
		}
	}

	if len(result.Messages) == 0 {
		return fmt.Sprintf("\n<You ran a Discord search for '%s' in the current server, but there were no matches.>\n", spec.Content), citemap
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n<You ran a Discord search for '%s' in the current server. Total matching messages: %d. Here are the top results. If these are not useful, try a different query. Use citing in your response when relevant, e.g. [1]>\n", spec.Content, result.TotalResults)
	if len(authorIDs) > 0 {
		fmt.Fprintf(&sb, "<Search author filter resolved to %d Discord user id(s)>\n", len(authorIDs))
	}
	if len(unresolvedAuthorFilters) > 0 {
		fmt.Fprintf(&sb, "<Could not resolve author filter(s): %s>\n", strings.Join(unresolvedAuthorFilters, ", "))
	}
	if len(channelIDs) > 0 {
		fmt.Fprintf(&sb, "<Search channel filter resolved to %d Discord channel id(s)>\n", len(channelIDs))
	}
	if len(unresolvedChannelFilters) > 0 {
		fmt.Fprintf(&sb, "<Could not resolve channel filter(s): %s>\n", strings.Join(unresolvedChannelFilters, ", "))
	}
	if len(mentionIDs) > 0 {
		fmt.Fprintf(&sb, "<Search mention filter resolved to %d Discord user id(s)>\n", len(mentionIDs))
	}
	if len(unresolvedMentionFilters) > 0 {
		fmt.Fprintf(&sb, "<Could not resolve mention filter(s): %s>\n", strings.Join(unresolvedMentionFilters, ", "))
	}
	if len(spec.Has) > 0 {
		fmt.Fprintf(&sb, "<Search has filter: %s>\n", strings.Join(stringSliceFromHasTypes(spec.Has), ", "))
	}
	if spec.Pinned != nil {
		fmt.Fprintf(&sb, "<Search pinned filter: %t>\n", *spec.Pinned)
	}
	if spec.MaxID != 0 {
		fmt.Fprintf(&sb, "<Search before message id: %s>\n", spec.MaxID.String())
	}
	if spec.MinID != 0 {
		fmt.Fprintf(&sb, "<Search after message id: %s>\n", spec.MinID.String())
	}
	for i, message := range result.Messages {
		channelName := channelNames[message.ChannelID]
		if channelName == "" {
			channelName = message.ChannelID.String()
		}
		content := strings.Join(strings.Fields(message.Content), " ")
		content = ellipsisTrim(content, 300)
		if content == "" {
			content = "[no text content]"
		}
		fmt.Fprintf(&sb, "---\n## Source [%d]\nChannel: #%s\nAuthor: %s\nTime: %s\nURL: %s\nContent: %s\n---\n",
			i+1,
			channelName,
			message.Author.EffectiveName(),
			message.CreatedAt.UTC().Format(time.RFC3339),
			message.JumpURL(),
			content,
		)
		citemap[i+1] = message.JumpURL()
	}

	if extra := result.TotalResults - len(result.Messages); extra > 0 {
		fmt.Fprintf(&sb, "<%d more results were omitted>\n", extra)
	}

	return sb.String(), citemap
}

func parseDiscordSearchSpec(query string) discordSearchSpec {
	spec := discordSearchSpec{Content: strings.Join(strings.Fields(query), " ")}
	spec.Content, spec.From = extractDiscordFilterValues(spec.Content, discordFromFilterRegexp)
	spec.Content, spec.In = extractDiscordFilterValues(spec.Content, discordInFilterRegexp)
	hasContent, hasValues := extractDiscordFilterValues(spec.Content, discordHasFilterRegexp)
	spec.Content = hasContent
	for _, value := range hasValues {
		spec.Has = append(spec.Has, discord.MessageSearchHasType(value))
	}
	spec.Content, spec.Mentions = extractDiscordFilterValues(spec.Content, discordMentionsRegexp)

	beforeContent, beforeValues := extractDiscordFilterValues(spec.Content, discordBeforeFilterRegexp)
	spec.Content = beforeContent
	if len(beforeValues) > 0 {
		if id, err := parseFirstSnowflake(beforeValues[0]); err == nil {
			spec.MaxID = id
		}
	}

	afterContent, afterValues := extractDiscordFilterValues(spec.Content, discordAfterFilterRegexp)
	spec.Content = afterContent
	if len(afterValues) > 0 {
		if id, err := parseFirstSnowflake(afterValues[0]); err == nil {
			spec.MinID = id
		}
	}

	pinnedContent, pinnedValues := extractDiscordFilterValues(spec.Content, discordPinnedFilterRegexp)
	spec.Content = pinnedContent
	if len(pinnedValues) > 0 {
		if value, ok := parseBoolFilterValue(pinnedValues[0]); ok {
			spec.Pinned = &value
		}
	}

	return spec
}

func extractDiscordFilterValues(query string, filterRegexp *regexp.Regexp) (string, []string) {
	matches := filterRegexp.FindAllStringSubmatch(query, -1)
	if len(matches) == 0 {
		return strings.Join(strings.Fields(query), " "), nil
	}

	var values []string
	for _, match := range matches {
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				values = append(values, strings.TrimSpace(match[i]))
				break
			}
		}
	}

	cleaned := filterRegexp.ReplaceAllString(query, " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned, values
}

func parseFirstSnowflake(value string) (snowflake.ID, error) {
	trimmed := normalizeDiscordSearchValue(value)
	if strings.HasPrefix(trimmed, "<@") && strings.HasSuffix(trimmed, ">") {
		trimmed = strings.TrimPrefix(trimmed, "<@")
		trimmed = strings.TrimPrefix(trimmed, "!")
		trimmed = strings.TrimSuffix(trimmed, ">")
	}
	if strings.HasPrefix(trimmed, "<#") && strings.HasSuffix(trimmed, ">") {
		trimmed = strings.TrimPrefix(trimmed, "<#")
		trimmed = strings.TrimSuffix(trimmed, ">")
	}
	return snowflake.Parse(trimmed)
}

func parseBoolFilterValue(value string) (bool, bool) {
	switch strings.ToLower(normalizeDiscordSearchValue(value)) {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func normalizeDiscordSearchValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func stringSliceFromHasTypes(values []discord.MessageSearchHasType) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func resolveDiscordAuthorIDs(client *bot.Client, guildID snowflake.ID, requesterID snowflake.ID, requesterName string, filters []string) ([]snowflake.ID, []string) {
	seen := map[snowflake.ID]struct{}{}
	var authorIDs []snowflake.ID
	var unresolved []string

	add := func(id snowflake.ID) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		authorIDs = append(authorIDs, id)
	}

	for _, raw := range filters {
		filter := normalizeDiscordSearchValue(raw)
		if filter == "" {
			continue
		}

		switch strings.ToLower(filter) {
		case "me", "@me", "myself", "self", "you":
			if requesterID != 0 {
				add(requesterID)
				continue
			}
			if requesterName != "" {
				filter = requesterName
			} else {
				unresolved = append(unresolved, raw)
				continue
			}
		}

		if id, err := parseFirstSnowflake(filter); err == nil {
			add(id)
			continue
		}

		matched := false
		for member := range client.Caches.Members(guildID) {
			if memberMatchesFilter(member, filter) {
				add(member.User.ID)
				matched = true
			}
		}
		if !matched {
			unresolved = append(unresolved, raw)
		}
	}

	return authorIDs, unresolved
}

func resolveDiscordChannelIDs(client *bot.Client, guildID snowflake.ID, filters []string) ([]snowflake.ID, []string) {
	if len(filters) == 0 {
		return nil, nil
	}

	channels, err := client.Rest.GetGuildChannels(guildID)
	if err != nil {
		return nil, filters
	}

	seen := map[snowflake.ID]struct{}{}
	var channelIDs []snowflake.ID
	var unresolved []string

	add := func(id snowflake.ID) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		channelIDs = append(channelIDs, id)
	}

	for _, raw := range filters {
		filter := normalizeDiscordSearchValue(raw)
		if filter == "" {
			continue
		}

		if id, err := parseFirstSnowflake(filter); err == nil {
			add(id)
			continue
		}

		matched := false
		nameFilter := strings.TrimPrefix(filter, "#")
		nameFilter = strings.ToLower(nameFilter)
		for _, channel := range channels {
			if strings.ToLower(strings.TrimSpace(channel.Name())) == nameFilter {
				add(channel.ID())
				matched = true
			}
		}
		if !matched {
			unresolved = append(unresolved, raw)
		}
	}

	return channelIDs, unresolved
}

func resolveDiscordUserIDs(client *bot.Client, guildID snowflake.ID, requesterID snowflake.ID, requesterName string, filters []string) ([]snowflake.ID, []string) {
	if len(filters) == 0 {
		return nil, nil
	}

	seen := map[snowflake.ID]struct{}{}
	var userIDs []snowflake.ID
	var unresolved []string

	add := func(id snowflake.ID) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		userIDs = append(userIDs, id)
	}

	for _, raw := range filters {
		filter := normalizeDiscordSearchValue(raw)
		if filter == "" {
			continue
		}

		switch strings.ToLower(filter) {
		case "me", "@me", "myself", "self", "you":
			if requesterID != 0 {
				add(requesterID)
				continue
			}
			if requesterName != "" {
				filter = requesterName
			} else {
				unresolved = append(unresolved, raw)
				continue
			}
		}

		if id, err := parseFirstSnowflake(filter); err == nil {
			add(id)
			continue
		}

		matched := false
		for member := range client.Caches.Members(guildID) {
			if memberMatchesFilter(member, filter) {
				add(member.User.ID)
				matched = true
			}
		}
		if !matched {
			unresolved = append(unresolved, raw)
		}
	}

	return userIDs, unresolved
}

func memberMatchesFilter(member discord.Member, filter string) bool {
	target := strings.ToLower(strings.TrimSpace(filter))
	if target == "" {
		return false
	}

	candidates := []string{
		member.EffectiveName(),
		member.User.EffectiveName(),
		member.User.Username,
		member.User.Tag(),
	}

	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == target {
			return true
		}
	}

	return false
}
