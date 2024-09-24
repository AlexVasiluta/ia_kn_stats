package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"go.uber.org/zap"
	csacademyscraper "vasiluta.ro/ia_kn_stats/csacademy_scraper"
	"vasiluta.ro/ia_kn_stats/ia_scraper"
	"vasiluta.ro/ia_kn_stats/scraper"
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
	csacademyFlag = flag.Bool("csacademy", false, "Add stats for csacademy")
	campionFlag   = flag.Bool("campion", false, "Add stats for campion.edu.ro")
)

func main() {
	flag.Parse()
	nerdarena, err := scraper.New("Nerdarena", "dump_nerdarena.db", &ia_scraper.IAParser{Host: "www.nerdarena.ro"})
	if err != nil {
		zap.S().Fatal(err)
	}

	infoarena, err := scraper.New("Infoarena", "dump.db", &ia_scraper.IAParser{Host: "www.infoarena.ro"})
	if err != nil {
		zap.S().Fatal(err)
	}

	csacademy, err := scraper.New("CSAcademy", "dump_csa.db", &csacademyscraper.CSAParser{})
	if err != nil {
		zap.S().Fatal(err)
	}

	//campion, err := scraper.New("Campion", "dump_campion.db", &campionscraper.CampionParser{})
	campion, err := scraper.New("Campion", "dump_campion.db", &ia_scraper.IAParser{Host: "invalid"})
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

	if *csacademyFlag {
		if err := csacademy.ParseNewSubs(context.Background()); err != nil {
			zap.S().Fatal(err)
		}
	}

	if *campionFlag {
		if err := campion.ParseNewSubs(context.Background()); err != nil {
			zap.S().Fatal(err)
		}
	}

	if *scrapeForward {
		if !(*infoarenaFlag || *nerdarenaFlag || *csacademyFlag || *campionFlag) {
			zap.S().Fatal("Cannot scrape forward if all fetching backends are disabled")
		}
		zap.S().Info("Scrape forward for extern backends. Press Ctrl+C to quit")
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
		if *csacademyFlag {
			go func() {
				if err := csacademy.ParseBacklog(ctx); err != nil {
					zap.S().Warn(err)
					stop()
				}
			}()
		}
		if *campionFlag {
			go func() {
				if err := campion.ParseBacklog(ctx); err != nil {
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
		stats := []*scraper.Statistics{}

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

		if *csacademyFlag {
			csaStats, err := csacademy.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				zap.S().Fatal(err)
			}
			stats = append(stats, csaStats)
		}

		if *campionFlag {
			campionStats, err := campion.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				zap.S().Fatal(err)
			}
			stats = append(stats, campionStats)
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
			ShowCSADisclaimer:     *csacademyFlag,
		}, f); err != nil {
			zap.S().Fatal(err)
		}
	}

}
