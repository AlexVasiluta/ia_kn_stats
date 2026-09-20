package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"vasiluta.ro/ia_kn_stats/algocode_scraper"
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

	dataDir = flag.String("data_dir", ".", "Data directory prefix")

	kilonovaFlag  = flag.Bool("kilonova", true, "Add stats for kilonova")
	infoarenaFlag = flag.Bool("infoarena", true, "Add stats for infoarena")
	algocodeFlag  = flag.Bool("algocode", true, "Add stats for algocode")
	nerdarenaFlag = flag.Bool("nerdarena", true, "Add stats for nerdarena")
	csacademyFlag = flag.Bool("csacademy", false, "Add stats for csacademy")
	campionFlag   = flag.Bool("campion", false, "Add stats for campion.edu.ro")
)

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to run program", slog.Any("error", err))
		os.Exit(1)
	}
}

func parseBacklog[Token any](ctx context.Context, stop func(), sc *scraper.Scraper[Token]) {
	if err := sc.ParseBacklog(ctx); err != nil {
		slog.WarnContext(ctx, "Error parsing backend", slog.Any("error", err), slog.String("backend", sc.Name()))
		stop()
	}
}

func run(ctx context.Context) error {
	flag.Parse()
	nerdarena, err := scraper.New("Nerdarena", filepath.Join(*dataDir, "dump_nerdarena.db"), &ia_scraper.IAParser{Host: "www.nerdarena.ro"})
	if err != nil {
		return err
	}

	infoarena, err := scraper.New("Infoarena", filepath.Join(*dataDir, "dump.db"), &ia_scraper.IAParser{Host: "infoarena.ro"})
	if err != nil {
		return err
	}

	algocode, err := scraper.New("AlgoCode", filepath.Join(*dataDir, "dump_algocode.db"), &algocode_scraper.AlgolympParser{Host: "https://code.algolymp.com/api/v2/public"})
	if err != nil {
		return err
	}
	csacademy, err := scraper.New("CSAcademy", filepath.Join(*dataDir, "dump_csa.db"), &csacademyscraper.CSAParser{})
	if err != nil {
		return err
	}

	//campion, err := scraper.New("Campion", filepath.Join(*dataDir, "dump_campion.db"), &campionscraper.CampionParser{})
	campion, err := scraper.New("Campion", filepath.Join(*dataDir, "dump_campion.db"), &ia_scraper.IAParser{Host: "invalid"})
	if err != nil {
		return err
	}

	var scrapeForwardEnabled bool

	if *nerdarenaFlag {
		scrapeForwardEnabled = true
		slog.InfoContext(ctx, "Parsing nerdarena")
		if err := nerdarena.ParseNewSubs(context.Background()); err != nil {
			return err
		}
	}

	if *infoarenaFlag {
		scrapeForwardEnabled = true
		slog.InfoContext(ctx, "Parsing infoarena")
		if err := infoarena.ParseNewSubs(context.Background()); err != nil {
			return err
		}
	}

	if *csacademyFlag {
		scrapeForwardEnabled = true
		slog.InfoContext(ctx, "Parsing csacademy")
		if err := csacademy.ParseNewSubs(context.Background()); err != nil {
			return err
		}
	}

	if *algocodeFlag {
		scrapeForwardEnabled = true
		slog.InfoContext(ctx, "Parsing algocode")
		if err := algocode.ParseNewSubs(context.Background()); err != nil {
			return err
		}
	}

	if *campionFlag {
		scrapeForwardEnabled = true
		slog.InfoContext(ctx, "Parsing campion")
		if err := campion.ParseNewSubs(context.Background()); err != nil {
			return err
		}
	}

	if *scrapeForward {
		if !scrapeForwardEnabled {
			return errors.New("cannot scrape forward if all fetching backends are disabled")
		}
		slog.InfoContext(ctx, "Scrape forward for extern backends. Press Ctrl+C to quit")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

		if *infoarenaFlag {
			go parseBacklog(ctx, stop, infoarena)
		}
		if *nerdarenaFlag {
			go parseBacklog(ctx, stop, nerdarena)
		}
		if *csacademyFlag {
			go parseBacklog(ctx, stop, csacademy)
		}
		if *campionFlag {
			go parseBacklog(ctx, stop, campion)
		}
		if *algocodeFlag {
			go parseBacklog(ctx, stop, algocode)
		}

		<-ctx.Done()
		slog.InfoContext(ctx, "Closing")
		return nil
	}

	if *exportStats {
		stats := []*scraper.Statistics{}

		if *kilonovaFlag {
			if *kilonovaDSN == "" {
				return errors.New("empty kilonova DSN")
			}

			knStats, err := GetKilonovaStats(context.Background(), *kilonovaDSN, *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, knStats)
		}

		if *infoarenaFlag {
			iaStats, err := infoarena.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, iaStats)
		}
		if *algocodeFlag {
			algocodeStats, err := algocode.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, algocodeStats)
		}

		if *nerdarenaFlag {
			naStats, err := nerdarena.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, naStats)
		}

		if *csacademyFlag {
			csaStats, err := csacademy.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, csaStats)
		}

		if *campionFlag {
			campionStats, err := campion.DB.GetInfoarenaStats(context.Background(), *exportDays, *exportMonths, *exportRollInterval, *exportRollingMonths)
			if err != nil {
				return err
			}
			stats = append(stats, campionStats)
		}

		f, err := os.Create(*exportStatsPath)
		if err != nil {
			return err
		}
		defer f.Close()

		return ExportToVROBody(context.Background(), &Config{
			Platforms:        stats,
			NumDays:          *exportDays,
			NumMonths:        *exportMonths,
			RollingInterval:  *exportRollInterval,
			NumRollingMonths: *exportRollingMonths,

			ShowWaitingDisclaimer: *infoarenaFlag || *nerdarenaFlag,
			ShowCSADisclaimer:     *csacademyFlag,
		}, f)
	}

	return nil
}
