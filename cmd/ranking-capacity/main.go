// ranking-capacity is an opt-in test tool, not part of the deployed service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cpa-usage-ranking/internal/capacity"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ranking-capacity seed|load|burst|inspect|verify|probe [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	db := flags.String("db", "", "synthetic SQLite path (seed requires a NEW file)")
	fixture := flags.String("fixture", "", "private synthetic fixture manifest")
	catalog := flags.String("models", "config/models.json", "formal model catalog")
	clients := flags.Int("clients", 10000, "synthetic client count")
	history := flags.Int("history-days", 90, "synthetic settled history days")
	backlog := flags.Int("backlog", 0, "clients whose newest historical day remains pending")
	day := flags.String("day", time.Now().UTC().Format(time.DateOnly), "UTC fixture day")
	base := flags.String("url", "http://127.0.0.1:4210", "test service origin")
	rate := flags.Float64("rps", 10, "open-loop offered request rate")
	duration := flags.Duration("duration", time.Minute, "bounded load duration")
	workers := flags.Int("workers", 64, "bounded HTTP concurrency")
	offset := flags.Int("offset", 0, "zero-based starting synthetic identity index")
	revision := flags.Int64("revision", 2, "first revision; must exceed previous runs")
	changed := flags.Int("changed", 2, "changed model rows per full snapshot, 1..8")
	both := flags.Bool("both-days", false, "alternate today/yesterday for each identity")
	ledger := flags.String("ledger", "", "NEW JSONL receipt path")
	ledgers := flags.String("ledgers", "", "comma-separated complete receipt history for verify")
	events := flags.String("events", "", "NEW burst logical-task events JSONL path")
	samples := flags.String("samples", "", "NEW burst one-second samples JSONL path")
	cycle := flags.Duration("cycle", 10*time.Minute, "burst background cycle for each identity")
	requestTimeout := flags.Duration("request-timeout", 20*time.Second, "burst per-attempt timeout")
	retrySpread := flags.Duration("retry-spread", time.Minute, "burst additional retry jitter spread")
	maxRetryDelay := flags.Duration("max-retry-delay", 2*time.Minute, "burst maximum retry delay")
	maxAttempts := flags.Int("max-attempts", 30, "burst maximum attempts per logical task")
	queue := flags.Int("queue-per-client", 16, "burst bounded pending queue per identity")
	maxPending := flags.Int("max-pending", 0, "burst total pending task limit (0 selects 4*clients)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	var value any
	var err error
	switch args[0] {
	case "seed":
		if *db == "" || *fixture == "" {
			return fmt.Errorf("seed requires --db and --fixture")
		}
		file, e := os.OpenFile(*fixture, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer file.Close()
		f, e := capacity.Seed(ctx, *db, *catalog, *clients, *history, *backlog, *day)
		if e != nil {
			return e
		}
		if e = json.NewEncoder(file).Encode(f); e != nil {
			return e
		}
		value, err = capacity.Inspect(ctx, *db)
	case "load":
		f, e := capacity.ReadFixture(*fixture)
		if e != nil {
			return e
		}
		value, err = capacity.Load(ctx, f, capacity.LoadOptions{URL: *base, RPS: *rate, Duration: *duration, Workers: *workers, Offset: *offset, Revision: *revision, Changed: *changed, BothDays: *both, Ledger: *ledger})
	case "burst":
		f, e := capacity.ReadFixture(*fixture)
		if e != nil {
			return e
		}
		if *maxPending == 0 {
			*maxPending = 4 * f.Clients
		}
		value, err = capacity.Burst(ctx, f, capacity.BurstOptions{URL: *base, Duration: *duration, Cycle: *cycle, RequestTimeout: *requestTimeout, RetrySpread: *retrySpread, MaxRetryDelay: *maxRetryDelay, MaxAttempts: *maxAttempts, QueuePerClient: *queue, MaxPending: *maxPending, Changed: *changed, Ledger: *ledger, Events: *events, Samples: *samples})
	case "inspect":
		value, err = capacity.Inspect(ctx, *db)
	case "verify":
		f, e := capacity.ReadFixture(*fixture)
		if e != nil {
			return e
		}
		if *ledgers == "" {
			return fmt.Errorf("verify requires --ledgers")
		}
		value, err = capacity.Verify(ctx, f, *db, strings.Split(*ledgers, ","))
	case "probe":
		value, err = capacity.Probe(ctx, *db, *day)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if value != nil {
		if e := json.NewEncoder(os.Stdout).Encode(value); e != nil {
			return e
		}
	}
	return err
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
