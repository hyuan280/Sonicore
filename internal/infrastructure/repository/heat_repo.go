package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/core/domain"
)

// HeatRepo owns the append-only track_events log and the cached tracks.heat
// aggregate. The cached heat is only moved when the underlying event row was
// actually inserted or deleted, so retries and repeated actions cannot inflate
// it.
//
// Idempotency of the dedupe_key unique index depends on the key being stable:
// favorite and playlist awards derive it from (user, playlist) and are fully
// idempotent. Play/completion awards embed the caller's session token; the
// browser path passes the Valkey listen-window token, so repeated reports of
// one listen are idempotent at the database layer too. The jukebox records once
// per finalized listen, and subsonic once per accepted scrobble. In all cases
// the Valkey marker (SET NX / listen accounting) is the primary guard against
// duplicate counting, so a Valkey flush could let a replayed request count
// again.
type HeatRepo struct {
	db *sql.DB
}

func NewHeatRepo(db *sql.DB) *HeatRepo {
	return &HeatRepo{db: db}
}

// execer is satisfied by both *sql.DB and *sql.Tx so an award can run inside a
// caller-managed transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// Award records a heat event and adds its weight to the cached aggregate. It
// returns true when a new event row was inserted (false for a duplicate).
func (r *HeatRepo) Award(ctx context.Context, trackID, userID, eventType string, weight int, dedupeKey string) (bool, error) {
	return r.awardTx(ctx, r.db, trackID, userID, eventType, weight, dedupeKey)
}

func (r *HeatRepo) awardTx(ctx context.Context, ex execer, trackID, userID, eventType string, weight int, dedupeKey string) (bool, error) {
	var uid interface{}
	if userID != "" {
		uid = userID
	}
	res, err := ex.ExecContext(ctx,
		`WITH ins AS (
			INSERT INTO track_events (id, track_id, user_id, event_type, weight, dedupe_key, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (dedupe_key) DO NOTHING
			RETURNING track_id, weight
		)
		UPDATE tracks t SET heat = t.heat + ins.weight FROM ins WHERE t.id = ins.track_id`,
		domain.NewID(), trackID, uid, eventType, weight, dedupeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Reverse removes a heat event and subtracts its weight from the aggregate. It
// is a no-op when the event does not exist. Returns true when a row was removed.
func (r *HeatRepo) Reverse(ctx context.Context, dedupeKey string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`WITH del AS (
			DELETE FROM track_events WHERE dedupe_key = $1 RETURNING track_id, weight
		)
		UPDATE tracks t SET heat = t.heat - del.weight FROM del WHERE t.id = del.track_id`,
		dedupeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ReverseMany reverses a batch of dedupe keys in one round trip.
func (r *HeatRepo) ReverseMany(ctx context.Context, dedupeKeys []string) error {
	if len(dedupeKeys) == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`WITH del AS (
			DELETE FROM track_events WHERE dedupe_key = ANY($1) RETURNING track_id, weight
		),
		agg AS (
			SELECT track_id, SUM(weight) AS total FROM del GROUP BY track_id
		)
		UPDATE tracks t SET heat = t.heat - agg.total FROM agg WHERE t.id = agg.track_id`,
		pq.Array(dedupeKeys))
	return err
}

// CountPlay records one playback instance. countPlay and countComplete are
// decided by the caller (Valkey rate limit + position thresholds). The play
// event, the cached heat and play_count/last_played_at are written in a single
// transaction; the completion bonus is an independent award keyed by the same
// session, so a completed listen is credited even when the play itself was
// already counted. Both awards are idempotent through their dedupe keys.
func (r *HeatRepo) CountPlay(ctx context.Context, trackID, userID, session string, countPlay, countComplete bool) (bool, error) {
	if !countPlay && !countComplete {
		return false, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	counted := false
	if countPlay {
		inserted, err := r.awardTx(ctx, tx, trackID, userID,
			domain.HeatEventPlay, domain.HeatWeightPlay,
			domain.PlaySessionKey(userID, trackID, session))
		if err != nil {
			return false, err
		}
		if inserted {
			counted = true
			if _, err := tx.ExecContext(ctx,
				`UPDATE tracks SET play_count = play_count + 1, last_played_at = NOW() WHERE id = $1`,
				trackID); err != nil {
				return false, err
			}
		}
	}

	if countComplete {
		if _, err := r.awardTx(ctx, tx, trackID, userID,
			domain.HeatEventPlayComplete, domain.HeatWeightPlayComplete,
			domain.PlayCompleteSessionKey(userID, trackID, session)); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return counted, nil
}

// RecordScrobble counts a play reported by an external client (Subsonic
// scrobble). The caller supplies a server-generated token after passing the
// rate limit. Completion is intentionally not awarded: a scrobble carries no
// position or duration, so a completed listen cannot be distinguished from a
// partial one.
func (r *HeatRepo) RecordScrobble(ctx context.Context, trackID, userID, session string) (bool, error) {
	return r.CountPlay(ctx, trackID, userID, session, true, false)
}

// RecomputeHeat rebuilds the cached heat for a single track from its event log.
func (r *HeatRepo) RecomputeHeat(ctx context.Context, trackID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks t SET heat = COALESCE(
			(SELECT SUM(e.weight) FROM track_events e WHERE e.track_id = t.id), 0)
		 WHERE t.id = $1`, trackID)
	return err
}

// RecomputeAll rebuilds the cached heat for every track from the event log.
func (r *HeatRepo) RecomputeAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks t SET heat = COALESCE(
			(SELECT SUM(e.weight) FROM track_events e WHERE e.track_id = t.id), 0)`)
	return err
}

// TrendingTrack is an aggregated event score for one track over a time window.
type TrendingTrack struct {
	TrackID string
	Score   int
}

// Trending returns the highest-scoring tracks since the given time, optionally
// restricted to a set of libraries. Reserved for a future trending endpoint.
func (r *HeatRepo) Trending(ctx context.Context, libraryIDs []string, since time.Time, limit int) ([]TrendingTrack, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT e.track_id, SUM(e.weight) AS score
		FROM track_events e
		JOIN tracks t ON t.id = e.track_id
		WHERE e.created_at >= $1`
	args := []interface{}{since, limit}
	if len(libraryIDs) > 0 {
		query += ` AND t.library_id = ANY($3)`
		args = append(args, pq.Array(libraryIDs))
	}
	query += ` GROUP BY e.track_id HAVING SUM(e.weight) > 0
		ORDER BY score DESC, e.track_id LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TrendingTrack
	for rows.Next() {
		var t TrendingTrack
		if err := rows.Scan(&t.TrackID, &t.Score); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
