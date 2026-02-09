package db

import (
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/persona"
)

type UserCache struct {
	// Personas are all the personas made or imported in /personamaker
	Personas []persona.TavernCardV1
}

func NewUserCache() *UserCache {
	return &UserCache{}
}

func unmarshalUserCache(data []byte) (*UserCache, error) {
	cache := &UserCache{}
	err := json.Unmarshal(data, cache)
	return cache, err
}

// write saves the UserCache state to the database for the given user ID.
func (cache UserCache) Write(id snowflake.ID) error {
	slog.Debug("writing user cache", slog.String("user_id", id.String()))
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT OR REPLACE INTO user_cache (user_id, cache) VALUES (?, ?)", id.String(), data)
	if err != nil {
		slog.Error("failed to write user cache to DB", "err", err, slog.String("user_id", id.String()))
	}
	return err
}

// GetUserCache retrieves the UserCache for a given ID from the database.
// It always returns a valid cache (a new one if not found or on error).
func GetUserCache(id snowflake.ID) *UserCache {
	var data []byte
	err := DB.QueryRow("SELECT cache FROM user_cache WHERE user_id = ?", id.String()).Scan(&data)
	if err != nil {
		if err != sql.ErrNoRows {
			// Log errors other than simply not found
			slog.Warn("failed to get user cache from DB", "err", err, slog.String("user_id", id.String()))
		}
		// Return a new cache if not found or on error
		return NewUserCache()
	}

	// Decode JSON
	cache, err := unmarshalUserCache(data)
	if err != nil {
		slog.Warn("failed to unmarshal user cache", "err", err, slog.String("user_id", id.String()))
		// Return a new cache and attempt to overwrite the corrupted one
		cache = NewUserCache()
		go cache.Write(id) // Attempt to fix the corrupted entry asynchronously
	}
	return cache
}
