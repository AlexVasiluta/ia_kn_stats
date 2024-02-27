package main

import (
	"context"
	"html/template"
	"io"
	"slices"
	"strconv"
	"strings"
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

const Kilonova = "Kilonova"

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
			$2 AS platform_name,
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $1
	`, numDays, Kilonova)
	if err != nil {
		return nil, err
	}

	monthStats, err := getStats(ctx, conn, `WITH starting_data AS (
		SELECT user_id, problem_id, DATE_TRUNC('month', created_at AT TIME ZONE 'UTC', 'UTC') AS day FROM submissions
	   ) SELECT 
	   		$2 AS platform_name,
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $1`, numMonths, Kilonova)
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
	   		$3 AS platform_name,
	   		COUNT(*) AS num_submissions, 
			COUNT(DISTINCT (user_id, problem_id)) AS excluding_multiple, 
			COUNT(DISTINCT user_id) AS unique_users, 
			COUNT(DISTINCT problem_id) AS unique_problems, 
			day AS time
			FROM starting_data GROUP BY day ORDER BY day DESC
		LIMIT $2`, strconv.Itoa(rollInterval), numRollingMonths, Kilonova)
	if err != nil {
		return nil, err
	}

	var lastTime time.Time
	if err := conn.QueryRow(ctx, "SELECT MAX(created_at) AT TIME ZONE 'UTC' FROM submissions").Scan(&lastTime); err != nil {
		return nil, err
	}

	return &ia_scraper.Statistics{
		PlatformName:   Kilonova,
		LastSubmission: lastTime,

		DayStats:           dayStats,
		RollingMonthsStats: rollingMonthStats,
		MonthsStats:        monthStats,
	}, nil
}

type daysStruct struct {
	DayUTC time.Time

	Platforms []*ia_scraper.StatsRow
}

type Config struct {
	Platforms []*ia_scraper.Statistics
	NumDays   int
	NumMonths int

	RollingInterval  int
	NumRollingMonths int

	ShowWaitingDisclaimer bool
}

func convertStats(platforms [][]*ia_scraper.StatsRow, order []string) []daysStruct {
	var days = make(map[time.Time]map[string]*ia_scraper.StatsRow)

	for _, platform := range platforms {
		for _, day := range platform {
			d := days[day.Time.UTC()]
			if d == nil {
				d = make(map[string]*ia_scraper.StatsRow)
			}
			d[day.PlatformName] = day
			days[day.Time.UTC()] = d
		}
	}

	var days2 []daysStruct
	for dUtc, val := range days {
		p := make([]*ia_scraper.StatsRow, len(order))
		for i := range order {
			day, ok := val[order[i]]
			if ok {
				p[i] = day
			}
		}
		days2 = append(days2, daysStruct{
			DayUTC:    dUtc,
			Platforms: p,
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
	var names []string
	for _, pl := range conf.Platforms {
		names = append(names, pl.PlatformName)
	}

	args := struct {
		LastUpdatedAt time.Time
		H1Name        string

		Platforms []*ia_scraper.Statistics

		NumDays   int
		NumMonths int

		RollingInterval  int
		NumRollingMonths int

		DaysStats          []daysStruct
		MonthsStats        []daysStruct
		RollingMonthsStats []daysStruct

		ShowWaitingDisclaimer bool
	}{
		H1Name: strings.Join(names, "/"),

		LastUpdatedAt: time.Now().UTC(),
		Platforms:     conf.Platforms,

		NumDays:   conf.NumDays,
		NumMonths: conf.NumMonths,

		RollingInterval:  conf.RollingInterval,
		NumRollingMonths: conf.NumRollingMonths,

		DaysStats:          convertStats(getDayStats(conf.Platforms)),
		MonthsStats:        convertStats(getMonthStats(conf.Platforms)),
		RollingMonthsStats: convertStats(getRollingMonthStats(conf.Platforms)),

		ShowWaitingDisclaimer: conf.ShowWaitingDisclaimer,
	}

	return templ.Execute(w, args)
}

func getDayStats(p []*ia_scraper.Statistics) ([][]*ia_scraper.StatsRow, []string) {
	rrows := make([][]*ia_scraper.StatsRow, len(p))
	order := make([]string, len(p))
	for i := range p {
		rrows[i] = p[i].DayStats
		order[i] = p[i].PlatformName
	}
	return rrows, order
}
func getMonthStats(p []*ia_scraper.Statistics) ([][]*ia_scraper.StatsRow, []string) {
	rrows := make([][]*ia_scraper.StatsRow, len(p))
	order := make([]string, len(p))
	for i := range p {
		rrows[i] = p[i].MonthsStats
		order[i] = p[i].PlatformName
	}
	return rrows, order
}
func getRollingMonthStats(p []*ia_scraper.Statistics) ([][]*ia_scraper.StatsRow, []string) {
	rrows := make([][]*ia_scraper.StatsRow, len(p))
	order := make([]string, len(p))
	for i := range p {
		rrows[i] = p[i].RollingMonthsStats
		order[i] = p[i].PlatformName
	}
	return rrows, order
}
