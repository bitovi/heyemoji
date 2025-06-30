package database

import (
	"database/sql"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type SQLiteDriver struct {
	path string
	db   *sql.DB
}

func NewSQLiteDriver(path string) Driver {
	return &SQLiteDriver{path: path}
}

func (d *SQLiteDriver) Open() error {
	db, err := sql.Open("sqlite3", filepath.Join(d.path, "karma.db"))
	if err != nil {
		return err
	}
	d.db = db
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS karma_events (
        id TEXT PRIMARY KEY,
        from_user TEXT,
        to_user TEXT,
        amount INTEGER,
        date TIMESTAMP
    )`)
	return err
}

func (d *SQLiteDriver) GiveKarma(to, from string, amount int, date time.Time) error {
	_, err := d.db.Exec(`INSERT INTO karma_events(id, from_user, to_user, amount, date) VALUES(?,?,?,?,?)`,
		uuid.New().String(), from, to, amount, date.UTC())
	return err
}

func (d *SQLiteDriver) QueryKarmaGiven(user string, since time.Time) (int, error) {
	row := d.db.QueryRow(`SELECT COALESCE(sum(amount),0) FROM karma_events WHERE from_user=? AND date>?`, user, since.UTC())
	var sum int
	if err := row.Scan(&sum); err != nil {
		return 0, err
	}
	return sum, nil
}

func (d *SQLiteDriver) QueryKarmaReceived(user string, since time.Time) (int, error) {
	row := d.db.QueryRow(`SELECT COALESCE(sum(amount),0) FROM karma_events WHERE to_user=? AND date>?`, user, since.UTC())
	var sum int
	if err := row.Scan(&sum); err != nil {
		return 0, err
	}
	return sum, nil
}

func (d *SQLiteDriver) QueryLeaderboard(since time.Time) (map[string]int, error) {
	rows, err := d.db.Query(`SELECT to_user, sum(amount) FROM karma_events WHERE date>? GROUP BY to_user`, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var user string
		var sum int
		if err := rows.Scan(&user, &sum); err != nil {
			return nil, err
		}
		result[user] = sum
	}
	return result, rows.Err()
}
