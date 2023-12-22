package main

import (
	"context"
	"html/template"
	"io"
	"slices"
	"strconv"
	"time"

	_ "embed"

	"github.com/jackc/pgx/v5"
	"vasiluta.ro/ia_kn_stats/ia_scraper"
)

//go:embed templ.body
var templData string

var (
	templ = template.Must(template.New("stats_body").Parse(templData))
)

func getStats(ctx context.Context, conn *pgx.Conn, query string, args ...any) ([]*ia_scraper.StatsRow, error) {
	rows, _ := conn.Query(ctx, query, args...)
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByNameLax[ia_scraper.StatsRow])
}

func GetKilonovaStats(ctx context.Context, dsn string, numDays, numMonths, rollInterval, numRollingMonths int) (*ia_scraper.Statistics, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.RuntimeParams["timezone"] = "UTC"
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	dayStats, err := getStats(ctx, conn, `WITH starting_data AS (
		SELECT user_id, problem_id, DATE_TRUNC('day', created_at AT TIME ZONE 'UTC', 'UTC') AS day FROM submissions
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $1
	`, numDays)
	if err != nil {
		return nil, err
	}

	monthStats, err := getStats(ctx, conn, `WITH starting_data AS (
		SELECT user_id, problem_id, DATE_TRUNC('month', created_at AT TIME ZONE 'UTC', 'UTC') AS day FROM submissions
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $1`, numMonths)
	if err != nil {
		return nil, err
	}

	rollingMonthStats, err := getStats(ctx, conn, `WITH starting_data AS (
		SELECT user_id, problem_id, 
			DATE_BIN(($1 || ' days')::interval,
				DATE_TRUNC('day', created_at AT TIME ZONE 'UTC', 'UTC'),
				DATE_TRUNC('day', NOW() AT TIME ZONE 'UTC', 'UTC') + '1 day'::interval	
			) AS day FROM submissions
	   ) SELECT 
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $2`, strconv.Itoa(rollInterval), numRollingMonths)
	if err != nil {
		return nil, err
	}

	var lastTime time.Time
	if err := conn.QueryRow(ctx, "SELECT MAX(created_at) AT TIME ZONE 'UTC' FROM submissions").Scan(&lastTime); err != nil {
		return nil, err
	}

	return &ia_scraper.Statistics{
		LastSubmission: lastTime,

		DayStats:           dayStats,
		RollingMonthsStats: rollingMonthStats,
		MonthsStats:        monthStats,
	}, nil
}

type daysStruct struct {
	DayUTC time.Time

	KNStats *ia_scraper.StatsRow
	IAStats *ia_scraper.StatsRow
}

type Config struct {
	KNStats   *ia_scraper.Statistics
	IAStats   *ia_scraper.Statistics
	NumDays   int
	NumMonths int

	RollingInterval  int
	NumRollingMonths int
}

func convertStats(knStats []*ia_scraper.StatsRow, iaStats []*ia_scraper.StatsRow) []daysStruct {
	var days = make(map[time.Time]struct {
		KNStats *ia_scraper.StatsRow
		IAStats *ia_scraper.StatsRow
	})

	for _, day := range knStats {
		d := days[day.Time.UTC()]
		d.KNStats = day
		days[day.Time.UTC()] = d
	}
	for _, day := range iaStats {
		d := days[day.Time.UTC()]
		d.IAStats = day
		days[day.Time.UTC()] = d
	}

	var days2 []daysStruct
	for dUtc, val := range days {
		days2 = append(days2, daysStruct{
			DayUTC:  dUtc,
			KNStats: val.KNStats,
			IAStats: val.IAStats,
		})
	}

	slices.SortFunc(days2, func(a, b daysStruct) int {
		if a.DayUTC.Equal(b.DayUTC) {
			return 0
		}
		if a.DayUTC.Before(b.DayUTC) {
			return 1
		}
		return -1
	})
	return days2
}

func ExportToVROBody(ctx context.Context, conf *Config, w io.Writer) error {
	args := struct {
		LastUpdatedAt time.Time
		KNStats       *ia_scraper.Statistics
		IAStats       *ia_scraper.Statistics

		NumDays   int
		NumMonths int

		RollingInterval  int
		NumRollingMonths int

		DaysStats          []daysStruct
		MonthsStats        []daysStruct
		RollingMonthsStats []daysStruct
	}{
		LastUpdatedAt: time.Now().UTC(),
		KNStats:       conf.KNStats,
		IAStats:       conf.IAStats,

		NumDays:   conf.NumDays,
		NumMonths: conf.NumMonths,

		RollingInterval:  conf.RollingInterval,
		NumRollingMonths: conf.NumRollingMonths,

		DaysStats:          convertStats(conf.KNStats.DayStats, conf.IAStats.DayStats),
		MonthsStats:        convertStats(conf.KNStats.MonthsStats, conf.IAStats.MonthsStats),
		RollingMonthsStats: convertStats(conf.KNStats.RollingMonthsStats, conf.IAStats.RollingMonthsStats),
	}

	return templ.Execute(w, args)
}
