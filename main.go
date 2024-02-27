package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"go.uber.org/zap"
	"vasiluta.ro/ia_kn_stats/ia_scraper"
)

var (
	scrapeForward   = flag.Bool("scrape_forward", false, "Whether to scrape forward in search of submissions")
	exportStats     = flag.Bool("export_stats", true, "Export stats to html file")
	exportStatsPath = flag.String("export_path", "./out.html", "Path to export stats to")
	exportDays      = flag.Int("export_days", 180, "Show stats from last x days")

	exportMonths        = flag.Int("export_months", 12, "Show stats from last x calendar months")
	exportRollingMonths = flag.Int("export_roll_months", 6, "Show stats from last x rolling month intervals")
	exportRollInterval  = flag.Int("export_roll_days", 30, "Number of days in rolling month interval")

	kilonovaDSN = flag.String("kilonova_dsn", "", "DSN to connect to kn database")

	kilonovaFlag  = flag.Bool("kilonova", true, "Add stats for kilonova")
	infoarenaFlag = flag.Bool("infoarena", true, "Add stats for infoarena")
	nerdarenaFlag = flag.Bool("nerdarena", true, "Add stats for nerdarena")
)

func main() {
	flag.Parse()

	nerdarena, err := ia_scraper.New("www.nerdarena.ro", "Nerdarena", "dump_nerdarena.db")
	if err != nil {
		zap.S().Fatal(err)
	}

	infoarena, err := ia_scraper.New("www.infoarena.ro", "Infoarena", "dump.db")
	if err != nil {
		zap.S().Fatal(err)
	}

	if *nerdarenaFlag {
		if err := nerdarena.ParseNewSubs(context.Background()); err != nil {
			zap.S().Fatal(err)
		}
	}

	if *infoarenaFlag {
		if err := infoarena.ParseNewSubs(context.Background()); err != nil {
			zap.S().Fatal(err)
		}
	}

	if *scrapeForward {
		if !(*infoarenaFlag || *nerdarenaFlag) {
			zap.S().Fatal("Cannot scrape forward if both infoarena and nerdarena are disabled")
		}
		zap.S().Info("Scrape forward for infoarena/nerdarena. Press Ctrl+C to quit")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		if *infoarenaFlag {
			go func() {
				if err := infoarena.ParseBacklog(ctx); err != nil {
					zap.S().Warn(err)
					stop()
				}
			}()
		}
		if *nerdarenaFlag {
			go func() {
				if err := nerdarena.ParseBacklog(ctx); err != nil {
					zap.S().Warn(err)
					stop()
				}
			}()
		}

		<-ctx.Done()
		zap.S().Info("Closing")
		os.Exit(0)
	}

	if *exportStats {
		stats := []*ia_scraper.Statistics{}

		if *kilonovaFlag {
			if *kilonovaDSN == "" {
				zap.S().Fatal("Empty kilonova DSN")
			}

			knStats, err := GetKilonovaStats(context.Background(), *kilonovaDSN, *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				zap.S().Fatal(err)
			}
			stats = append(stats, knStats)
		}

		if *infoarenaFlag {
			iaStats, err := infoarena.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				zap.S().Fatal(err)
			}
			stats = append(stats, iaStats)
		}

		if *nerdarenaFlag {
			naStats, err := nerdarena.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				zap.S().Fatal(err)
			}
			stats = append(stats, naStats)
		}

		f, err := os.Create(*exportStatsPath)
		if err != nil {
			zap.S().Fatal(err)
		}
		defer f.Close()

		if err := ExportToVROBody(context.Background(), &Config{
			Platforms:        stats,
			NumDays:          *exportDays,
			NumMonths:        *exportMonths,
			RollingInterval:  *exportRollInterval,
			NumRollingMonths: *exportRollingMonths,

			ShowWaitingDisclaimer: *infoarenaFlag || *nerdarenaFlag,
		}, f); err != nil {
			zap.S().Fatal(err)
		}
	}

}
