package ia_scraper

import (
	"context"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

type IASubmission struct {
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

type Scraper struct {
	DB   *DB
	Host string
}

func (sc *Scraper) ParseNewSubs(ctx context.Context) error {
	// Keep inserting until finding first submission that was already inserted
	offset := 0
	for {
		subs, err := ParseMonitorPage(ctx, sc.Host, offset, nil)
		if err != nil {
			zap.S().Warn(err)
			return err
		}
		numInserted, err := sc.DB.InsertMonitorPage(ctx, subs)
		if err != nil {
			zap.S().Warn(err)
			continue
		}
		zap.S().Info(offset, numInserted)
		if numInserted == 0 {
			break
		}
		offset += entriesCount
	}
	return nil
}

func (sc *Scraper) ParseBacklog(ctx context.Context) error {
	newOffset, err := sc.DB.CountSubmissions(ctx)
	if err != nil {
		panic(err)
	}
	for {
		subs, err := ParseMonitorPage(ctx, sc.Host, newOffset, nil)
		if err != nil {
			zap.S().Warn(err)
			continue
		}
		ok, err := sc.DB.SubmissionExists(ctx, subs[0].ID)
		if err != nil {
			zap.S().Warn(err)
			ok = false
		}
		if ok {
			break
		}
		newOffset -= entriesCount
	}
	zap.S().Infof("Starting offset for long scrape (%s): %d", sc.DB.PlatformName, newOffset)
	for {
		if int(newOffset/100)*100%1000 == 0 {
			zap.S().Infof("Offset (%s): %d", sc.DB.PlatformName, newOffset)
		}
		subs, err := ParseMonitorPage(ctx, sc.Host, newOffset, nil)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				zap.S().Info("Quitting for ", sc.DB.PlatformName)
				return nil
			}
			zap.S().Warn(err)
			continue
		}
		if _, err := sc.DB.InsertMonitorPage(ctx, subs); err != nil {
			if errors.Is(err, context.Canceled) {
				zap.S().Info("Quitting for ", sc.DB.PlatformName)
				return nil
			}
			zap.S().Warn(err)
			continue
		}
		newOffset += entriesCount
		if len(subs) == 0 {
			zap.S().Infof("(%s) Found page with no more submissions, might have reached the end", sc.DB.PlatformName)
			return nil
		}
	}
}

func New(host, name, dbname string) (*Scraper, error) {
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

	db := &DB{db: d, PlatformName: name}
	return &Scraper{db, host}, nil
}
