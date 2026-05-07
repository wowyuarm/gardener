package search

import (
	"net/url"
	"strings"
)

// parseMagnet extracts infohash and trackers from a magnet URI, and returns a
// sanitized magnet containing only udp/http/https trackers (anacrolix/torrent
// panics on unknown schemes like wss://).
func parseMagnet(magnet string) (infohash string, trackers []string, sanitized string) {
	u, err := url.Parse(magnet)
	if err != nil {
		return "", nil, magnet
	}
	q := u.Query()
	xt := q.Get("xt")
	if strings.HasPrefix(xt, "urn:btih:") {
		infohash = strings.ToLower(strings.TrimPrefix(xt, "urn:btih:"))
	}
	for _, tr := range q["tr"] {
		if isSupportedTrackerScheme(tr) {
			trackers = append(trackers, tr)
		}
	}
	// Rebuild with only supported trackers
	q.Del("tr")
	for _, tr := range trackers {
		q.Add("tr", tr)
	}
	u.RawQuery = q.Encode()
	sanitized = u.String()
	return
}

func isSupportedTrackerScheme(u string) bool {
	lu := strings.ToLower(u)
	return strings.HasPrefix(lu, "udp://") ||
		strings.HasPrefix(lu, "http://") ||
		strings.HasPrefix(lu, "https://")
}
