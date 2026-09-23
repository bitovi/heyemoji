// Package postgres implements database.Driver on top of Postgres (via pgx), replacing
// the legacy JSONL flat-file store.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the "pgx5" migrate scheme
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bitovi/heyemoji/database"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func New(connString string) *Driver {
	return &Driver{connString: connString}
}

type Driver struct {
	connString string
	pool       *pgxpool.Pool
}

func (d *Driver) Open(ctx context.Context) error {
	if err := d.runMigrations(); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	pool, err := pgxpool.New(ctx, d.connString)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging postgres: %w", err)
	}
	d.pool = pool
	return nil
}

func (d *Driver) runMigrations() error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	// The migrate library's pgx driver is registered under the "pgx5" URL scheme
	// (see its init()), so rewrite whatever scheme DATABASE_URL uses.
	migrateURL := d.connString
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(migrateURL, scheme) {
			migrateURL = "pgx5://" + strings.TrimPrefix(migrateURL, scheme)
			break
		}
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// GiveKarma inserts events atomically, holding a per-giver advisory lock for the
// duration of the transaction so the cap check below can't race with a concurrent
// give from the same user (the old JSONL driver's in-process mutex provided this for
// free; Postgres needs an explicit lock).
func (d *Driver) GiveKarma(ctx context.Context, events []database.KarmaEvent, since time.Time, dailyCap int) ([]database.KarmaEvent, int, error) {
	if len(events) == 0 {
		return nil, 0, nil
	}
	from := events[0].From

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, from); err != nil {
		return nil, 0, err
	}

	var alreadyGiven int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(points), 0) FROM karma_events WHERE from_user = $1 AND created_at > $2`,
		from, since,
	).Scan(&alreadyGiven); err != nil {
		return nil, 0, err
	}

	batchTotal := 0
	for _, ev := range events {
		batchTotal += ev.Points
	}

	if alreadyGiven+batchTotal > dailyCap {
		return nil, 0, database.ErrDailyCapExceeded
	}

	batch := &pgx.Batch{}
	for _, ev := range events {
		batch.Queue(
			`INSERT INTO karma_events (from_user, to_user, emoji, points, reason, source_channel_id, thread_channel_id, thread_ts)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			ev.From, ev.To, ev.Emoji, ev.Points, ev.Reason, ev.SourceChannelID, nullable(ev.ThreadChannelID), nullable(ev.ThreadTS),
		)
	}

	inserted := make([]database.KarmaEvent, len(events))
	copy(inserted, events)

	br := tx.SendBatch(ctx, batch)
	for i := range events {
		if err := br.QueryRow().Scan(&inserted[i].ID); err != nil {
			br.Close()
			return nil, 0, err
		}
	}
	if err := br.Close(); err != nil {
		return nil, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}

	return inserted, dailyCap - (alreadyGiven + batchTotal), nil
}

// LatestThreadTS returns the thread_ts of the most recent event for (threadChannelID,
// toUser) created after since, if any.
func (d *Driver) LatestThreadTS(ctx context.Context, threadChannelID, toUser string, since time.Time) (string, bool, error) {
	var ts string
	err := d.pool.QueryRow(ctx, `
		SELECT thread_ts FROM karma_events
		WHERE thread_channel_id = $1 AND to_user = $2 AND created_at > $3 AND thread_ts IS NOT NULL
		ORDER BY created_at DESC LIMIT 1`,
		threadChannelID, toUser, since,
	).Scan(&ts)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return ts, true, nil
}

// SetThreadTS backfills thread_ts on the given event IDs.
func (d *Driver) SetThreadTS(ctx context.Context, eventIDs []string, ts string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	_, err := d.pool.Exec(ctx, `UPDATE karma_events SET thread_ts = $1 WHERE id = ANY($2)`, ts, eventIDs)
	return err
}

// nullable converts an empty string to a nil interface{} so it's stored as SQL NULL
// rather than an empty string.
func nullable(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (d *Driver) QueryKarmaGiven(ctx context.Context, user string, since time.Time) (int, error) {
	var total int
	err := d.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(points), 0) FROM karma_events WHERE from_user = $1 AND created_at > $2`,
		user, since,
	).Scan(&total)
	return total, err
}

func (d *Driver) QueryKarmaReceived(ctx context.Context, user string, since time.Time) (int, error) {
	var total int
	err := d.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(points), 0) FROM karma_events WHERE to_user = $1 AND created_at > $2`,
		user, since,
	).Scan(&total)
	return total, err
}

// QueryFeed returns the most recent recognition events (newest first), up to limit.
func (d *Driver) QueryFeed(ctx context.Context, limit int) ([]database.KarmaEvent, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, from_user, to_user, emoji, points, COALESCE(reason, ''), COALESCE(source_channel_id, ''), created_at
		FROM karma_events
		ORDER BY created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []database.KarmaEvent
	for rows.Next() {
		var ev database.KarmaEvent
		if err := rows.Scan(&ev.ID, &ev.From, &ev.To, &ev.Emoji, &ev.Points, &ev.Reason, &ev.SourceChannelID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// QueryEventsSince returns every event created after since, oldest first.
func (d *Driver) QueryEventsSince(ctx context.Context, since time.Time) ([]database.KarmaEvent, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, from_user, to_user, emoji, points, COALESCE(reason, ''), COALESCE(source_channel_id, ''), created_at
		FROM karma_events
		WHERE created_at > $1
		ORDER BY created_at ASC`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []database.KarmaEvent
	for rows.Next() {
		var ev database.KarmaEvent
		if err := rows.Scan(&ev.ID, &ev.From, &ev.To, &ev.Emoji, &ev.Points, &ev.Reason, &ev.SourceChannelID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (d *Driver) QueryLeaderboard(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT to_user, SUM(points) FROM karma_events WHERE created_at > $1 GROUP BY to_user`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanLeaderboardRows(rows)
}

// QueryLeaderboardRange is QueryLeaderboard bounded on both ends.
func (d *Driver) QueryLeaderboardRange(ctx context.Context, since, until time.Time) (map[string]int, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT to_user, SUM(points) FROM karma_events WHERE created_at > $1 AND created_at <= $2 GROUP BY to_user`,
		since, until,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanLeaderboardRows(rows)
}

func scanLeaderboardRows(rows pgx.Rows) (map[string]int, error) {
	result := map[string]int{}
	for rows.Next() {
		var user string
		var points int
		if err := rows.Scan(&user, &points); err != nil {
			return nil, err
		}
		result[user] = points
	}
	return result, rows.Err()
}

// QueryEarliestEventTime returns the created_at of the very first event ever
// recorded, if any. MIN() over an empty table returns a NULL row (not zero rows),
// hence scanning into a *time.Time rather than time.Time directly.
func (d *Driver) QueryEarliestEventTime(ctx context.Context) (time.Time, bool, error) {
	var t *time.Time
	if err := d.pool.QueryRow(ctx, `SELECT MIN(created_at) FROM karma_events`).Scan(&t); err != nil {
		return time.Time{}, false, err
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return *t, true, nil
}
