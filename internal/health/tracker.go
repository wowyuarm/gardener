package health

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/tracker"
	"github.com/anacrolix/torrent/types/infohash"
)

type TrackerResult struct {
	URL      string
	Seeders  int
	Leechers int
	Err      error
}

// ScrapeTrackers scrapes each tracker URL in parallel and returns one result per tracker.
// Per-tracker timeout is enforced via ctx with timeout.
func ScrapeTrackers(ctx context.Context, infoHash string, trackers []string, perTrackerTimeout time.Duration) []TrackerResult {
	ih, err := parseInfoHash(infoHash)
	if err != nil {
		return nil
	}

	results := make([]TrackerResult, len(trackers))
	var wg sync.WaitGroup
	for i, u := range trackers {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			results[i] = scrapeOne(ctx, u, ih, perTrackerTimeout)
		}(i, u)
	}
	wg.Wait()
	return results
}

func scrapeOne(parent context.Context, trackerURL string, ih infohash.T, timeout time.Duration) TrackerResult {
	res := TrackerResult{URL: trackerURL}
	if !isSupportedScheme(trackerURL) {
		res.Err = errors.New("unsupported scheme")
		return res
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cl, err := tracker.NewClient(trackerURL, tracker.NewClientOpts{})
	if err != nil {
		res.Err = err
		return res
	}
	defer cl.Close()

	resp, err := cl.Scrape(ctx, []infohash.T{ih})
	if err != nil {
		res.Err = err
		return res
	}
	if len(resp) == 0 {
		res.Err = errors.New("empty scrape response")
		return res
	}
	res.Seeders = int(resp[0].Seeders)
	res.Leechers = int(resp[0].Leechers)
	return res
}

func isSupportedScheme(u string) bool {
	lu := strings.ToLower(u)
	return strings.HasPrefix(lu, "udp://") || strings.HasPrefix(lu, "http://") || strings.HasPrefix(lu, "https://")
}

func parseInfoHash(s string) (infohash.T, error) {
	var ih infohash.T
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 20 {
		return ih, errors.New("invalid infohash")
	}
	copy(ih[:], b)
	return ih, nil
}

// AvgSeeders returns the average seeder count across responsive trackers (Err == nil).
// Returns 0 if no tracker responded.
func AvgSeeders(rs []TrackerResult) float64 {
	var sum, n int
	for _, r := range rs {
		if r.Err != nil {
			continue
		}
		sum += r.Seeders
		n++
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}
