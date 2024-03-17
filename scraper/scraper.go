package scraper

import (
	"context"
	"errors"

	"go.uber.org/zap"
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
		zap.S().Info(offset, numInserted)
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
	zap.S().Infof("Starting offset for long scrape (%s): %v", sc.DB.PlatformName, offset)
	for {
		// if offset... {
		// 	zap.S().Infof("Offset (%s): %d", sc.DB.PlatformName, newOffset)
		// }
		subs, err := sc.parser.GetPage(ctx, offset)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				zap.S().Info("Quitting for ", sc.DB.PlatformName)
				return nil
			}
			zap.S().Warn(err)
			continue
		}
		if len(subs) == 0 {
			zap.S().Infof("(%s) Found page with no more submissions, might have reached the end", sc.DB.PlatformName)
			return nil
		}
		if _, err := sc.DB.InsertMonitorPage(ctx, subs); err != nil {
			if errors.Is(err, context.Canceled) {
				zap.S().Info("Quitting for ", sc.DB.PlatformName)
				return nil
			}
			zap.S().Warn(err)
			continue
		}
		offset, err = sc.parser.FurthestOffset(ctx, sc.DB)
		if err != nil {
			zap.S().Warn(err)
		}
	}
}

func New[Token any](name, dbname string, parser Parser[Token]) (*Scraper[Token], error) {
	db, err := NewDB(name, dbname)
	if err != nil {
		return nil, err
	}
	return &Scraper[Token]{db, parser}, nil
}
