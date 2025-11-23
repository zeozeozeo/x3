package db

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

// DB should be initialized via commands.InitDB

const (
	// DefaultContextMessages is the default number of surrounding messages used for LLM context.
	DefaultContextMessages = 60
)

// ChannelCache holds per-channel settings and temporary state.
type ChannelCache struct {
	// Llmer is used for caching message history in channels where the bot cannot read messages (e.g., DMs).
	Llmer *llm.Llmer `json:"llmer"`
	// PersonaMeta stores the persona configuration (name, model, system prompt, settings).
	PersonaMeta persona.PersonaMeta `json:"persona_meta"`
	// Usage tracks token usage for this channel.
	Usage llm.Usage `json:"usage"`
	// LastUsage tracks token usage for the very last interaction.
	LastUsage llm.Usage `json:"last_usage"`
	// ContextLength is the number of surrounding messages to use as context.
	ContextLength int `json:"context_length"`
	// LastInteraction records the timestamp of the last interaction in this channel.
	LastInteraction time.Time `json:"last_interaction"`
	// KnownNonDM indicates if we've confirmed this channel is not a DM (optimization for random DMs).
	KnownNonDM bool `json:"known_non_dm,omitempty"`
	// NoRandomDMs indicates if the user has opted out of random DMs in this channel.
	NoRandomDMs bool `json:"no_random_dms,omitempty"`
	// EverUsedRandomDMs tracks if the /random_dms command was ever used.
	EverUsedRandomDMs bool `json:"ever_used_random_dms,omitempty"`
	// IsLastRandomDM indicates if the last message sent by the bot was a random DM interaction.
	IsLastRandomDM bool `json:"is_last_random_dm,omitempty"`
	// Summary is an LLM-defined summary of the message history.
	Summary persona.Summary `json:"summary,omitzero"`
	// Context is a list of user-defined context strings.
	Context []string `json:"context,omitempty"`
	// MessagesSinceSummary tracks the number of messages since the last summary update.
	MessagesSinceSummary int `json:"messages_since_summary"`
}

func (cache *ChannelCache) UpdateSummary(summary persona.Summary) {
	summary.Str = strings.TrimSpace(summary.Str)
	summary.Age = 1
	if summary.Str != "" {
		cache.Summary = summary
	}
}

// updateInteractionTime updates the LastInteraction timestamp to now.
func (cache *ChannelCache) UpdateInteractionTime() {
	cache.LastInteraction = time.Now()
}

// NewChannelCache creates a ChannelCache with default values.
func NewChannelCache() *ChannelCache {
	return &ChannelCache{PersonaMeta: persona.PersonaProto.DeepCopy(), ContextLength: DefaultContextMessages}
}

// unmarshalChannelCache decodes JSON data into a ChannelCache struct, applying defaults.
func unmarshalChannelCache(data []byte) (*ChannelCache, error) {
	cache := ChannelCache{
		ContextLength: DefaultContextMessages,
		PersonaMeta:   persona.PersonaProto.DeepCopy(),
	}
	err := json.Unmarshal(data, &cache)
	if cache.ContextLength == 0 {
		cache.ContextLength = DefaultContextMessages
	}
	if cache.PersonaMeta.Name == "" {
		cache.PersonaMeta = persona.PersonaProto.DeepCopy()
	} else {
		cache.PersonaMeta.Settings = cache.PersonaMeta.Settings.Fixup()
	}

	cache.PersonaMeta.Migrate()

	return &cache, err
}

// write saves the ChannelCache state to the database for the given channel ID.
func (cache ChannelCache) Write(id snowflake.ID) error {
	slog.Debug("writing channel cache", slog.String("channel_id", id.String()))
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT OR REPLACE INTO channel_cache (channel_id, cache) VALUES (?, ?)", id.String(), data)
	if err != nil {
		slog.Error("failed to write channel cache to DB", "err", err, slog.String("channel_id", id.String()))
	}
	return err
}

// GetChannelCache retrieves the ChannelCache for a given ID from the database.
// It always returns a valid cache (a new one if not found or on error).
func GetChannelCache(id snowflake.ID) *ChannelCache {
	var data []byte
	err := DB.QueryRow("SELECT cache FROM channel_cache WHERE channel_id = ?", id.String()).Scan(&data)
	if err != nil {
		if err != sql.ErrNoRows {
			// Log errors other than simply not found
			slog.Warn("failed to get channel cache from DB", "err", err, slog.String("channel_id", id.String()))
		}
		// Return a new cache if not found or on error
		return NewChannelCache()
	}

	// Decode JSON
	cache, err := unmarshalChannelCache(data)
	if err != nil {
		slog.Warn("failed to unmarshal channel cache", "err", err, slog.String("channel_id", id.String()))
		// Return a new cache and attempt to overwrite the corrupted one
		cache = NewChannelCache()
		go cache.Write(id) // Attempt to fix the corrupted entry asynchronously
	}
	return cache
}

// GetCachedChannelIDs retrieves all channel IDs that have an entry in the channel_cache table.
func GetCachedChannelIDs() ([]snowflake.ID, error) {
	rows, err := DB.Query("SELECT channel_id FROM channel_cache")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []snowflake.ID
	for rows.Next() {
		var id snowflake.ID
		if err := rows.Scan(&id); err != nil {
			// Log error and continue if possible? Or return error immediately?
			slog.Error("Failed to scan channel ID from cache table", "err", err)
			continue // Skip this row
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return ids, err // Return potentially partial list along with iteration error
	}

	return ids, nil
}
