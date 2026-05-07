package main

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/joway/gardener/internal/health"
	"github.com/joway/gardener/internal/output"
	"github.com/joway/gardener/internal/search"
)

type options struct {
	limit        int
	jsonOut      bool
	verbose      bool
	provider     string
	overall      time.Duration
	dhtTimeout   time.Duration
	trkTimeout   time.Duration
	verifyBudget time.Duration
}

func main() {
	var opts options
	root := &cobra.Command{
		Use:   "gardener <query>",
		Short: "Search BT torrents and rank them by verified swarm health.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := joinArgs(args)
			return run(query, &opts)
		},
	}
	root.Flags().IntVarP(&opts.limit, "limit", "n", 20, "max torrents to test")
	root.Flags().BoolVar(&opts.jsonOut, "json", false, "emit JSON instead of a table")
	root.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "show site-reported seed/peer columns")
	root.Flags().StringVar(&opts.provider, "provider", "torrentz2", "search provider: torrentz2, nyaa")
	root.Flags().DurationVar(&opts.overall, "timeout", 90*time.Second, "overall wall-clock budget")
	root.Flags().DurationVar(&opts.dhtTimeout, "dht-timeout", 15*time.Second, "per-torrent DHT lookup budget")
	root.Flags().DurationVar(&opts.trkTimeout, "tracker-timeout", 8*time.Second, "per-tracker scrape timeout")
	root.Flags().DurationVar(&opts.verifyBudget, "verify-timeout", 25*time.Second, "per-torrent handshake/metadata budget")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gardener:", err)
		os.Exit(1)
	}
}

func joinArgs(a []string) string {
	out := a[0]
	for _, s := range a[1:] {
		out += " " + s
	}
	return out
}

func run(query string, opts *options) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.overall)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	provider, err := pickProvider(opts.provider)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "→ searching %s for %q (top %d)…\n", provider.Name(), query, opts.limit)
	results, err := provider.Search(ctx, query, opts.limit)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if len(results) == 0 {
		return fmt.Errorf("no torrents found for %q", query)
	}
	fmt.Fprintf(os.Stderr, "  got %d candidates\n", len(results))

	dhtSrv, err := health.NewDHT()
	if err != nil {
		return fmt.Errorf("dht init: %w", err)
	}
	defer dhtSrv.Close()

	bootstrapCtx, bootstrapCancel := context.WithTimeout(ctx, 10*time.Second)
	// Bootstrap returns context.DeadlineExceeded once it times out — that's expected;
	// the routing table is functional well before the traversal would naturally finish.
	_ = dhtSrv.Bootstrap(bootstrapCtx)
	bootstrapCancel()

	verifier, err := health.NewVerifier()
	if err != nil {
		return fmt.Errorf("verifier init: %w", err)
	}
	defer verifier.Close()

	reports := make([]*health.Report, len(results))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // up to 4 torrents in parallel; verifier client handles concurrency internally too
	for i, r := range results {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, r search.SearchResult) {
			defer wg.Done()
			defer func() { <-sem }()
			reports[i] = checkOne(ctx, dhtSrv, verifier, r, opts)
			rep := reports[i]
			fmt.Fprintf(os.Stderr, "  ✓ %s — verified=%d dht=%d trk_avg=%.0f score=%.1f\n",
				truncateForLog(rep.Name, 50), rep.Verify.VerifiedSeeds, rep.DHTPeers, rep.TrackerSeedsAvg, rep.Score)
		}(i, r)
	}
	wg.Wait()

	out := make([]*health.Report, 0, len(reports))
	for _, r := range reports {
		if r != nil {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if opts.jsonOut {
		return output.RenderJSON(os.Stdout, out)
	}
	return output.RenderTable(os.Stdout, out, opts.verbose)
}

func pickProvider(name string) (search.Provider, error) {
	switch name {
	case "torrentz2", "":
		return search.NewTorrentz2(), nil
	case "nyaa":
		return search.NewNyaa(), nil
	}
	return nil, fmt.Errorf("unknown provider %q (try torrentz2, nyaa)", name)
}

func checkOne(ctx context.Context, d *health.DHT, v *health.Verifier, r search.SearchResult, opts *options) *health.Report {
	rep := &health.Report{
		InfoHash:      r.InfoHash,
		Name:          r.Name,
		SizeBytes:     r.SizeBytes,
		Magnet:        r.Magnet,
		Added:         r.Added,
		ReportedSeeds: r.ReportedSeeds,
		ReportedPeers: r.ReportedPeers,
	}

	var (
		wg       sync.WaitGroup
		dhtPeers []netip.AddrPort
	)
	wg.Add(2)

	go func() {
		defer wg.Done()
		dhtCtx, cancel := context.WithTimeout(ctx, opts.dhtTimeout)
		defer cancel()
		peers, err := d.FindPeers(dhtCtx, r.InfoHash)
		if err != nil {
			return
		}
		rep.DHTPeers = len(peers)
		dhtPeers = peers
	}()

	go func() {
		defer wg.Done()
		rep.TrackerResults = health.ScrapeTrackers(ctx, r.InfoHash, r.Trackers, opts.trkTimeout)
		rep.TrackerSeedsAvg = health.AvgSeeders(rep.TrackerResults)
	}()
	wg.Wait()

	verifyCtx, cancel := context.WithTimeout(ctx, opts.verifyBudget)
	defer cancel()
	rep.Verify = v.Verify(verifyCtx, r.Magnet, dhtPeers)

	rep.Score = health.Score(rep)
	return rep
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
