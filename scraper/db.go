package scraper

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

type Submission struct {
	ID int

	Username    string
	DisplayName string

	ProblemID   *string
	ProblemName *string

	SizeKB *float64
	Date   time.Time

	Ignored       bool
	CompileError  bool
	InternalError bool
	Score         *int

	Handled bool
}

func InsertSubmission(ctx context.Context, execer sqlx.ExecerContext, sub *Submission) (bool, error) {
	_, err := execer.ExecContext(ctx,
		`INSERT INTO submissions (id, username, display_name, problem_id, problem_name, size_kb, date, ignored, compile_error, internal_error, score) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.Username, sub.DisplayName, sub.ProblemID, sub.ProblemName, sub.SizeKB, sub.Date, sub.Ignored, sub.CompileError, sub.InternalError, sub.Score,
	)
	if err != nil {
		var err2 sqlite3.Error
		if errors.As(err, &err2) {
			if err2.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
				// Still do insert or replace (to make sure up to date) but mark as having already inserted
				execer.ExecContext(ctx,
					`INSERT OR REPLACE INTO submissions (id, username, display_name, problem_id, problem_name, size_kb, date, ignored, compile_error, internal_error, score) 
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					sub.ID, sub.Username, sub.DisplayName, sub.ProblemID, sub.ProblemName, sub.SizeKB, sub.Date, sub.Ignored, sub.CompileError, sub.InternalError, sub.Score,
				)
				return false, nil
			}
		}
		return false, err
	}
	return true, err
}

type DB struct {
	db *sqlx.DB

	PlatformName string
}

func (s *DB) InsertMonitorPage(ctx context.Context, subs []*Submission) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var numInserted int
	for _, sub := range subs {
		if !sub.Handled {
			continue
		}
		ok, err := InsertSubmission(ctx, tx, sub)
		if err != nil {
			zap.S().Warn(err)
			continue
		}
		if ok {
			numInserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return numInserted, nil
}

func (s *DB) CountSubmissions(ctx context.Context) (int, error) {
	var cnt int
	err := s.db.GetContext(ctx, &cnt, "SELECT COUNT(*) FROM submissions")
	return cnt, err
}

func (s *DB) SubmissionExists(ctx context.Context, id int) (bool, error) {
	var cnt int
	err := s.db.GetContext(ctx, &cnt, "SELECT COUNT(*) FROM submissions WHERE id = ?", id)
	return cnt > 0, err
}

func NewDB(platformName string, dbname string) (*DB, error) {
	d, err := sqlx.Connect("sqlite3", dbname)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(`
CREATE TABLE IF NOT EXISTS submissions (
	id   INTEGER PRIMARY KEY,

	username TEXT NOT NULL,
	display_name TEXT NOT NULL,

	problem_id TEXT,
	problem_name TEXT,

	size_kb REAL,
	date TEXT NOT NULL,

	ignored BOOLEAN NOT NULL DEFAULT FALSE,
	compile_error BOOLEAN NOT NULL DEFAULT FALSE,
	internal_error BOOLEAN NOT NULL DEFAULT FALSE,
	score INTEGER
);	
`); err != nil {
		return nil, err
	}

	return &DB{db: d, PlatformName: platformName}, nil
}

type StatsRow struct {
	PlatformName string `json:"platform_name" db:"platform_name"`

	// Trimmed down to yyyy-mm-dd, no hours/minutes
	Time time.Time `json:"time"`

	SQLiteTime *string `json:"-" db:"sqlite_time"`
	SQLiteVar  any     `json:"" db:"var"`

	// Number of total submissions
	NumSubmissions int `json:"num_subs" db:"num_submissions"`

	// Number of unique (user, problem) pairs for submission
	ExcludingMultiple int `json:"excluding_multiple" db:"excluding_multiple"`

	// Number of unique users
	UniqueUsers int `json:"unique_users" db:"unique_users"`
	// Number of unique problems
	UniqueProblems int `json:"unique_pbs" db:"unique_problems"`
}

type Statistics struct {
	PlatformName string `json:"platform_name"`

	LastSubmission time.Time `json:"last_sub"`

	DayStats []*StatsRow `json:"day_stats"`

	RollingMonthsStats []*StatsRow `json:"rolling_month_stats"`

	MonthsStats []*StatsRow `json:"month_stats"`
}

func (s *DB) GetFurthestTime(ctx context.Context) (*time.Time, error) {
	var t sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT DATETIME(MIN(date)) FROM submissions").Scan(&t); err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	tt, err := time.ParseInLocation(time.DateTime, t.String, time.UTC)
	if err != nil {
		panic(err)
	}
	return &tt, nil
}

func (s *DB) getStats(ctx context.Context, query string, args ...any) ([]*StatsRow, error) {
	var stats []*StatsRow
	if err := s.db.SelectContext(ctx, &stats, query, args...); err != nil {
		return nil, err
	}

	for i := range stats {
		t, err := time.ParseInLocation(time.DateOnly, *stats[i].SQLiteTime, time.UTC)
		if err != nil {
			panic(err)
		}
		stats[i].PlatformName = s.PlatformName
		stats[i].Time = t
		stats[i].SQLiteTime = nil
	}
	return stats, nil
}

func (s *DB) GetInfoarenaStats(ctx context.Context, numDays, numMonths, rollInterval, numRollingMonths int) (*Statistics, error) {
	dayStats, err := s.getStats(ctx, `
	WITH starting_data AS (
		SELECT username, problem_id, DATE(subs.date, 'utc') AS day FROM submissions subs
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT username || '###' || problem_id) AS excluding_multiple, 
			COUNT(DISTINCT username) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS sqlite_time
		FROM starting_data GROUP BY day ORDER BY day DESC 
		LIMIT ?`, numDays)
	if err != nil {
		return nil, err
	}

	monthStats, err := s.getStats(ctx, `	
	WITH starting_data AS (
		SELECT username, problem_id, DATE(subs.date, 'utc', 'start of month') AS day FROM submissions subs
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT username || '###' || problem_id) AS excluding_multiple, 
			COUNT(DISTINCT username) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS sqlite_time
		FROM starting_data GROUP BY day ORDER BY day DESC 
		LIMIT ?`, numMonths)
	if err != nil {
		return nil, err
	}

	rollingMonthStats, err := s.getStats(ctx, `	
	WITH starting_data AS (
		SELECT username, problem_id, DATE(subs.date, 'utc') AS day FROM submissions subs
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT username || '###' || problem_id) AS excluding_multiple, 
			COUNT(DISTINCT username) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			MIN(day) AS sqlite_time,
			(unixepoch(DATE('now', 'utc')) - unixepoch(day)) / (86400 * ?) AS var
		FROM starting_data GROUP BY var ORDER BY day DESC 
		LIMIT ?`, rollInterval, numRollingMonths)
	if err != nil {
		return nil, err
	}

	var lastTime int64
	if err := s.db.GetContext(ctx, &lastTime, "SELECT MAX(unixepoch(date)) FROM submissions"); err != nil {
		return nil, err
	}

	return &Statistics{
		PlatformName: s.PlatformName,

		LastSubmission: time.Unix(lastTime, 0).UTC(),

		DayStats:           dayStats,
		RollingMonthsStats: rollingMonthStats,
		MonthsStats:        monthStats,
	}, nil
}
