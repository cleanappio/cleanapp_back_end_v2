package main

import (
	"cleanapp-common/appenv"
	"cleanapp-common/evidence"
	"context"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"os/signal"
	"report-analysis-backfill/evidencebackfill"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	brand := flag.String("brand", "", "single normalized brand")
	brands := flag.String("brands", "", "comma-separated normalized brands")
	limit := flag.Int("limit", 50, "maximum reports (1-10000)")
	after := flag.Int("after-seq", 0, "resume after sequence")
	dryRun := flag.Bool("dry-run", false, "classify and print without database writes (default behavior)")
	apply := flag.Bool("apply", false, "explicitly write additive evidence fields")
	force := flag.Bool("force", false, "reclassify previously enriched reports")
	interval := flag.Duration("interval", 3*time.Second, "pause between reports, minimum 250ms")
	flag.Parse()
	if *apply && *dryRun {
		return fmt.Errorf("--apply and --dry-run are mutually exclusive")
	}
	if *interval < 250*time.Millisecond {
		return fmt.Errorf("--interval must be at least 250ms")
	}
	requested := []string{}
	seen := map[string]bool{}
	for _, name := range strings.Split(*brand+","+*brands, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && !seen[name] {
			requested = append(requested, name)
			seen[name] = true
		}
	}
	opts := evidencebackfill.Options{Brands: requested, Limit: *limit, AfterSeq: *after, Apply: *apply, Force: *force, Interval: *interval}
	if err := opts.Validate(); err != nil {
		return err
	}
	var classifier evidence.Classifier
	switch appenv.String("ANALYZER_LLM_PROVIDER", "openai") {
	case "gemini":
		key, err := appenv.Secret("GEMINI_API_KEY", "")
		if err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("GEMINI_API_KEY is required")
		}
		classifier = evidence.NewGeminiClassifier(key, appenv.String("GEMINI_MODEL", "gemini-flash-latest"), nil)
	case "openai":
		key, err := appenv.Secret("OPENAI_API_KEY", "")
		if err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("OPENAI_API_KEY is required")
		}
		classifier = evidence.NewOpenAIClassifier(key, appenv.String("OPENAI_MODEL", "gpt-4o"), nil)
	default:
		return fmt.Errorf("ANALYZER_LLM_PROVIDER must be openai or gemini")
	}
	password, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return err
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", appenv.String("DB_USER", "server"), password, appenv.String("DB_HOST", "localhost"), appenv.String("DB_PORT", "3306"), appenv.String("DB_NAME", "cleanapp"))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}
	result, err := evidencebackfill.Run(ctx, opts, evidencebackfill.DatabaseStore{DB: db}, classifier, os.Stdout)
	if err == nil && result.Failed > 0 {
		return fmt.Errorf("%d reports could not be classified; failed rows were not written", result.Failed)
	}
	return err
}
