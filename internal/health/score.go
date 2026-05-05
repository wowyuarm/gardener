package health

import "time"

// Report aggregates all health signals for one torrent.
type Report struct {
	InfoHash         string
	Name             string
	SizeBytes        int64
	Magnet           string
	Added            time.Time
	ReportedSeeds    int
	ReportedPeers    int
	DHTPeers         int
	TrackerResults   []TrackerResult
	TrackerSeedsAvg  float64
	Verify           VerifyResult
	Score            float64
}

// AgeFactor returns a decay weight based on torrent age. Recent torrents get a small
// bonus; very old ones decay toward 0. We use 1 / (1 + years).
func AgeFactor(added time.Time) float64 {
	if added.IsZero() {
		return 0
	}
	years := time.Since(added).Hours() / 24 / 365
	if years < 0 {
		years = 0
	}
	return 1.0 / (1.0 + years)
}

// Score computes the weighted health score per the agreed formula:
//
//	score = 10 * verified_seeders
//	      +  2 * dht_peers
//	      +  1 * tracker_seeders_avg
//	      +  0.5 * age_factor
func Score(r *Report) float64 {
	return 10*float64(r.Verify.VerifiedSeeds) +
		2*float64(r.DHTPeers) +
		1*r.TrackerSeedsAvg +
		0.5*AgeFactor(r.Added)
}
