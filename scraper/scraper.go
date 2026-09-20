package scraper

import (
	"context"
	"errors"
	"log/slog"
)

type Parser[Offset any] interface {
	GetPage(ctx context.Context, token Offset) ([]*Submission, error)

	PageZeroOffset() Offset
	FurthestOffset(ctx context.Context, db *DB) (Offset, error)

	NextPageOffset(t Offset, subs []*Submission) Offset
}

type Scraper[Token any] struct {
	DB *DB

	parser Parser[Token]
	name   string
}

func (sc *Scraper[Token]) ParseNewSubs(ctx context.Context) error {
	offset := sc.parser.PageZeroOffset()
	for {
		subs, err := sc.parser.GetPage(ctx, offset)
		if err != nil {
			return err
		}
		numInserted, err := sc.DB.InsertMonitorPage(ctx, subs)
		if err != nil {
			continue
		}
		slog.InfoContext(ctx, "Parsing new page", slog.Any("offset", offset), slog.Int("numInserted", numInserted))
		if numInserted == 0 {
			break
		}
		offset = sc.parser.NextPageOffset(offset, subs)
	}
	return nil
}

func (sc *Scraper[Token]) ParseBacklog(ctx context.Context) error {
	offset, err := sc.parser.FurthestOffset(ctx, sc.DB)
	if err != nil {
		panic(err)
	}
	slog.InfoContext(ctx, "Starting long scrape", slog.String("name", sc.DB.PlatformName), slog.Any("offset", offset))
	for {
		subs, err := sc.parser.GetPage(ctx, offset)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				slog.InfoContext(ctx, "Quitting backlog parser", slog.String("name", sc.DB.PlatformName))
				return nil
			}
			slog.WarnContext(ctx, "Could not get page", slog.String("name", sc.DB.PlatformName), slog.Any("error", err))
			continue
		}
		if len(subs) == 0 {
			slog.InfoContext(ctx, "Found page with no more submissions, might have reached the end", slog.String("name", sc.DB.PlatformName))
			return nil
		}
		if _, err := sc.DB.InsertMonitorPage(ctx, subs); err != nil {
			if errors.Is(err, context.Canceled) {
				slog.InfoContext(ctx, "Quitting backlog parser", slog.String("name", sc.DB.PlatformName))
				return nil
			}
			slog.WarnContext(ctx, "Could not insert page", slog.String("name", sc.DB.PlatformName), slog.Any("error", err))
			continue
		}
		offset, err = sc.parser.FurthestOffset(ctx, sc.DB)
		if err != nil {
			slog.WarnContext(ctx, "Could not refetch furthest offset", slog.String("name", sc.DB.PlatformName), slog.Any("error", err))
		}
	}
}

func (sc *Scraper[Token]) Name() string {
	return sc.name
}

func New[Token any](name, dbname string, parser Parser[Token]) (*Scraper[Token], error) {
	db, err := NewDB(name, dbname)
	if err != nil {
		return nil, err
	}
	return &Scraper[Token]{db, parser, name}, nil
}
