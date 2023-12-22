package main

import (
	"context"
	"flag"
	"os"

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
)

func main() {
	flag.Parse()

	sc, err := ia_scraper.New()
	if err != nil {
		zap.S().Fatal(err)
	}

	if err := sc.ParseNewSubs(context.Background()); err != nil {
		zap.S().Fatal(err)
	}

	if *scrapeForward {
		zap.S().Info("Scrape forward for infoarena. Press Ctrl+C to quit")
		if err := sc.ParseBacklog(context.Background()); err != nil {
			zap.S().Fatal(err)
		}
	}

	if *exportStats {
		if *kilonovaDSN == "" {
			zap.S().Fatal("Empty kilonova DSN")
		}
		iaStats, err := sc.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
		if err != nil {
			zap.S().Fatal(err)
		}

		knStats, err := GetKilonovaStats(context.Background(), *kilonovaDSN, *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
		if err != nil {
			zap.S().Fatal(err)
		}
		// spew.Dump(knStats)

		f, err := os.Create(*exportStatsPath)
		if err != nil {
			zap.S().Fatal(err)
		}
		defer f.Close()

		if err := ExportToVROBody(context.Background(), &Config{
			KNStats:          knStats,
			IAStats:          iaStats,
			NumDays:          *exportDays,
			NumMonths:        *exportMonths,
			RollingInterval:  *exportRollInterval,
			NumRollingMonths: *exportRollingMonths,
		}, f); err != nil {
			zap.S().Fatal(err)
		}
	}

}
