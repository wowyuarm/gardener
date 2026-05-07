package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"

	"github.com/wowyuarm/gardener/internal/health"
)

func RenderTable(w io.Writer, reports []*health.Report, verbose bool) error {
	t := tablewriter.NewWriter(w)
	if verbose {
		t.Header("#", "name", "size", "added", "verified", "dht", "trk_avg", "rep_s/p", "score", "magnet")
	} else {
		t.Header("#", "name", "size", "added", "verified", "dht", "trk_avg", "score", "magnet")
	}
	for i, r := range reports {
		row := []string{
			fmt.Sprintf("%d", i+1),
			truncate(r.Name, 60),
			humanSize(r.SizeBytes),
			ageString(r.Added),
			fmt.Sprintf("%d", r.Verify.VerifiedSeeds),
			fmt.Sprintf("%d", r.DHTPeers),
			fmt.Sprintf("%.1f", r.TrackerSeedsAvg),
		}
		if verbose {
			row = append(row, fmt.Sprintf("%d/%d", r.ReportedSeeds, r.ReportedPeers))
		}
		row = append(row, fmt.Sprintf("%.1f", r.Score), shortMagnet(r.InfoHash))
		if err := t.Append(row); err != nil {
			return err
		}
	}
	return t.Render()
}

func shortMagnet(infoHash string) string {
	return "magnet:?xt=urn:btih:" + strings.ToUpper(infoHash)
}

func RenderJSON(w io.Writer, reports []*health.Report) error {
	type out struct {
		Rank             int     `json:"rank"`
		Name             string  `json:"name"`
		InfoHash         string  `json:"infohash"`
		Magnet           string  `json:"magnet"`
		SizeBytes        int64   `json:"size_bytes"`
		Added            string  `json:"added,omitempty"`
		ReportedSeeders  int     `json:"reported_seeders"`
		ReportedPeers    int     `json:"reported_peers"`
		DHTPeers         int     `json:"dht_peers"`
		TrackerSeedsAvg  float64 `json:"tracker_seeders_avg"`
		TrackerCount     int     `json:"tracker_count"`
		TrackerOK        int     `json:"tracker_ok"`
		GotMetadata      bool    `json:"got_metadata"`
		NumPieces        int     `json:"num_pieces"`
		ActiveConns      int     `json:"active_conns"`
		VerifiedSeeders  int     `json:"verified_seeders"`
		PartialPeers     int     `json:"partial_peers"`
		Score            float64 `json:"score"`
	}
	rows := make([]out, 0, len(reports))
	for i, r := range reports {
		ok := 0
		for _, tr := range r.TrackerResults {
			if tr.Err == nil {
				ok++
			}
		}
		var added string
		if !r.Added.IsZero() {
			added = r.Added.Format(time.RFC3339)
		}
		rows = append(rows, out{
			Rank:            i + 1,
			Name:            r.Name,
			InfoHash:        r.InfoHash,
			Magnet:          r.Magnet,
			SizeBytes:       r.SizeBytes,
			Added:           added,
			ReportedSeeders: r.ReportedSeeds,
			ReportedPeers:   r.ReportedPeers,
			DHTPeers:        r.DHTPeers,
			TrackerSeedsAvg: r.TrackerSeedsAvg,
			TrackerCount:    len(r.TrackerResults),
			TrackerOK:       ok,
			GotMetadata:     r.Verify.GotMetadata,
			NumPieces:       r.Verify.NumPieces,
			ActiveConns:     r.Verify.ActiveConns,
			VerifiedSeeders: r.Verify.VerifiedSeeds,
			PartialPeers:    r.Verify.PartialPeers,
			Score:           r.Score,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func humanSize(b int64) string {
	const unit = 1024.0
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := float64(b) / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	return fmt.Sprintf("%.2f %s", float64(b)/div, suffix)
}

func ageString(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
