// ranking-capacity is an opt-in test tool, not part of the deployed service.
package main

import (
	"bufio"
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
	"cpa-usage-ranking/internal/models"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ranking-capacity seed|seed-sustained|load|burst|sustained|assess-sustained|summarize-sustained|inspect|verify|verify-sustained|probe [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	db := flags.String("db", "", "synthetic SQLite path (seed requires a NEW file)")
	fixture := flags.String("fixture", "", "private synthetic fixture manifest")
	catalog := flags.String("models", "config/models.json", "formal model catalog")
	clients := flags.Int("clients", 10000, "synthetic client count")
	history := flags.Int("history-days", 90, "synthetic settled history days")
	backlog := flags.Int("backlog", 0, "clients whose newest historical day remains pending")
	day := flags.String("day", time.Now().UTC().Format(time.DateOnly), "UTC fixture day")
	modelSeed := flags.Uint64("model-seed", 1, "reproducible sustained synthetic content seed (not a credential)")
	arrivalSeed := flags.Uint64("arrival-seed", 2, "reproducible independent arrival-clock seed")
	journal := flags.String("journal", "", "private sustained JSONL journal")
	seedDB := flags.String("seed-db", "", "original stopped sustained seed SQLite database")
	expectedDB := flags.String("expected-db", "", "NEW local reconstruction database for sustained verification")
	resume := flags.Bool("resume", false, "resume a cleanly closed sustained journal, initially paused")
	retryMigration := flags.String("reviewed-retry-migration", "", "explicit reviewed C54 retry migration manifest")
	maxInFlight := flags.Int("max-in-flight", 4096, "local sustained simultaneous HTTP-attempt safety bound")
	maxJournal := flags.Int64("max-journal-bytes", 4<<30, "local sustained journal safety bound")
	maxArrivalLag := flags.Duration("max-arrival-lag", 5*time.Second, "stop if sustained input generation lags this far")
	activeBudget := flags.Duration("active-budget", time.Hour, "cumulative sustained active-input ceiling, at most one hour")
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
	samples := flags.String("samples", "", "NEW burst or sustained one-second samples JSONL path")
	cycle := flags.Duration("cycle", 10*time.Minute, "burst background cycle for each identity")
	requestTimeout := flags.Duration("request-timeout", 20*time.Second, "burst per-attempt timeout")
	retrySpread := flags.Duration("retry-spread", time.Minute, "burst additional retry jitter spread")
	maxRetryDelay := flags.Duration("max-retry-delay", 2*time.Minute, "burst maximum retry delay")
	maxAttempts := flags.Int("max-attempts", 30, "burst or sustained maximum attempts per logical task")
	queue := flags.Int("queue-per-client", 16, "burst bounded pending queue per identity")
	maxPending := flags.Int("max-pending", 0, "pending limit (0 selects burst 4*clients or sustained 200000)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	var value any
	var err error
	switch args[0] {
	case "seed", "seed-sustained":
		if *db == "" || *fixture == "" {
			return fmt.Errorf("seed requires --db and --fixture")
		}
		file, e := os.OpenFile(*fixture, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer file.Close()
		var f capacity.Fixture
		if args[0] == "seed-sustained" {
			f, e = capacity.SeedSustained(ctx, *db, *catalog, *clients, *history, *backlog, *day, *modelSeed)
		} else {
			f, e = capacity.Seed(ctx, *db, *catalog, *clients, *history, *backlog, *day)
		}
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
	case "sustained":
		f, e := capacity.ReadFixture(*fixture)
		if e != nil {
			return e
		}
		c, e := models.Load(*catalog)
		if e != nil {
			return e
		}
		var names []string
		for _, m := range c.List() {
			names = append(names, m.Name)
		}
		if *maxPending == 0 {
			*maxPending = 200000
		}
		commands := make(chan capacity.SustainedCommand)
		go func() {
			defer close(commands)
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				var command capacity.SustainedCommand
				if json.Unmarshal(scanner.Bytes(), &command) != nil {
					command.Operation = "invalid_json"
				}
				select {
				case commands <- command:
				case <-ctx.Done():
					return
				}
			}
			if scanner.Err() != nil {
				select {
				case commands <- capacity.SustainedCommand{Operation: "input_error"}:
				case <-ctx.Done():
				}
			}
		}()
		value, err = runSustainedSamples(ctx, f, capacity.SustainedOptions{URL: *base, Journal: *journal, Models: names, ModelSeed: *modelSeed, ArrivalSeed: *arrivalSeed, MaxPending: *maxPending, MaxInFlight: *maxInFlight, MaxAttempts: *maxAttempts, MaxJournalBytes: *maxJournal, RequestTimeout: *requestTimeout, MaxArrivalLag: *maxArrivalLag, MaxActive: *activeBudget, Resume: *resume, RetryMigrationPath: *retryMigration}, commands, *samples)
	case "assess-sustained":
		value, err = assessSustainedInput(os.Stdin)
	case "summarize-sustained":
		c, e := models.Load(*catalog)
		if e != nil {
			return e
		}
		var names []string
		for _, m := range c.List() {
			names = append(names, m.Name)
		}
		if *maxPending == 0 {
			*maxPending = 200000
		}
		value, err = capacity.SummarizeSustained(*journal, names, *maxPending, *retryMigration)
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
	case "verify-sustained":
		f, e := capacity.ReadFixture(*fixture)
		if e != nil {
			return e
		}
		c, e := models.Load(*catalog)
		if e != nil {
			return e
		}
		var names []string
		for _, m := range c.List() {
			names = append(names, m.Name)
		}
		value, err = capacity.VerifySustained(ctx, f, capacity.SustainedVerifyOptions{SeedDB: *seedDB, FinalDB: *db, Journal: *journal, ExpectedDB: *expectedDB, RetryMigrationPath: *retryMigration, Models: names, ModelSeed: *modelSeed})
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
