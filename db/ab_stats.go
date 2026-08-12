package db

import (
	"fmt"
)

// ABTestStat is the aggregate result for one default/A-B model pair.
type ABTestStat struct {
	DefaultModel string `json:"default_model"`
	ABModel      string `json:"ab_model"`
	Comparisons  uint64 `json:"comparisons"`
	AVotes       uint64 `json:"a_votes"`
	BVotes       uint64 `json:"b_votes"`
	Closed       uint64 `json:"closed"`
	Unanswered   uint64 `json:"unanswered"`
}

// ABStats contains the pair breakdown and global totals for modeled.
type ABStats struct {
	Pairs  []ABTestStat `json:"pairs"`
	Totals ABTestStat   `json:"totals"`
}

func RecordABComparison(defaultModel, abModel string) error {
	_, err := DB.Exec(`
		INSERT INTO ab_test_stats (default_model, ab_model, comparisons)
		VALUES (?, ?, 1)
		ON CONFLICT(default_model, ab_model) DO UPDATE SET comparisons = comparisons + 1`,
		defaultModel, abModel)
	return err
}

func RecordABVote(defaultModel, abModel string, response rune) error {
	column := "a_votes"
	if response == 'B' {
		column = "b_votes"
	}
	if response != 'A' && response != 'B' {
		return fmt.Errorf("invalid A/B response %q", response)
	}

	// The row is created when the prompt is sent. Keep this update atomic so
	// simultaneous button clicks cannot overwrite one another.
	_, err := DB.Exec(fmt.Sprintf(`
		UPDATE ab_test_stats SET %s = %s + 1
		WHERE default_model = ? AND ab_model = ?`, column, column), defaultModel, abModel)
	return err
}

func RecordABClose(defaultModel, abModel string) error {
	_, err := DB.Exec(`
		UPDATE ab_test_stats SET closed = closed + 1
		WHERE default_model = ? AND ab_model = ?`, defaultModel, abModel)
	return err
}

func GetABTestStats() (ABStats, error) {
	if DB == nil {
		return ABStats{}, fmt.Errorf("database is not initialized")
	}

	rows, err := DB.Query(`
		SELECT default_model, ab_model, comparisons, a_votes, b_votes, closed
		FROM ab_test_stats
		ORDER BY comparisons DESC, default_model, ab_model`)
	if err != nil {
		return ABStats{}, err
	}
	defer rows.Close()

	stats := ABStats{}
	for rows.Next() {
		var row ABTestStat
		var comparisons, aVotes, bVotes, closed int64
		if err := rows.Scan(&row.DefaultModel, &row.ABModel, &comparisons, &aVotes, &bVotes, &closed); err != nil {
			return ABStats{}, err
		}
		row.Comparisons = uint64(max(comparisons, 0))
		row.AVotes = uint64(max(aVotes, 0))
		row.BVotes = uint64(max(bVotes, 0))
		row.Closed = uint64(max(closed, 0))
		row.Unanswered = unanswered(row.Comparisons, row.AVotes, row.BVotes, row.Closed)
		stats.Pairs = append(stats.Pairs, row)
		stats.Totals.Comparisons += row.Comparisons
		stats.Totals.AVotes += row.AVotes
		stats.Totals.BVotes += row.BVotes
		stats.Totals.Closed += row.Closed
	}
	if err := rows.Err(); err != nil {
		return ABStats{}, err
	}
	stats.Totals.Unanswered = unanswered(stats.Totals.Comparisons, stats.Totals.AVotes, stats.Totals.BVotes, stats.Totals.Closed)
	return stats, nil
}

func unanswered(comparisons, aVotes, bVotes, closed uint64) uint64 {
	completed := aVotes + bVotes + closed
	if completed >= comparisons {
		return 0
	}
	return comparisons - completed
}
