package db

import "log/slog"

const legacyCacheDropKey = "legacy_cache_drop_v1"

// DropLegacyCaches removes the cache tables replaced by cache.Store and
// vacuums the database so the freed pages return to the filesystem. It is
// idempotent and records completion in maintenance_meta.
func DropLegacyCaches() (bool, error) {
	if DB == nil || maintenanceDone(legacyCacheDropKey) {
		return false, nil
	}

	var existed int
	if err := DB.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('image_descriptions', 'link_metadata')`,
	).Scan(&existed); err != nil {
		return false, err
	}

	if _, err := DB.Exec(`DROP TABLE IF EXISTS image_descriptions`); err != nil {
		return false, err
	}
	if _, err := DB.Exec(`DROP TABLE IF EXISTS link_metadata`); err != nil {
		return false, err
	}

	if err := withWorldWrite(func() error {
		_, err := DB.Exec("VACUUM")
		return err
	}); err != nil {
		return false, err
	}

	if _, err := DB.Exec(
		`INSERT INTO maintenance_meta(key, value) VALUES (?, 'done')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		legacyCacheDropKey,
	); err != nil {
		return false, err
	}
	slog.Info("legacy cache tables dropped", "tables_existed", existed > 0)
	return existed > 0, nil
}
