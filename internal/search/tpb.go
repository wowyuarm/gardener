package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	tpbBase     = "https://pirateproxy.live"
	tpbUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// TPBSearch is a search provider backed by pirateproxy.live (The Pirate Bay proxy).
// Covers Western movies, TV shows, software, games, and general content.
// No Cloudflare protection.
type TPBSearch struct {
	client *http.Client
}

func NewTPBSearch() *TPBSearch {
	return &TPBSearch{client: &http.Client{Timeout: 20 * time.Second}}
}

func (t *TPBSearch) Name() string { return "tpb" }

func (t *TPBSearch) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	// TPB search URL: /search/<query>/1/99/0 (sort by seeders desc)
	// Use PathEscape (not QueryEscape) because + in path is literal, not a space.
	u := fmt.Sprintf("%s/search/%s/1/99/0", tpbBase, url.PathEscape(query))
	doc, err := t.fetchDoc(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch search: %w", err)
	}

	var results []SearchResult
	doc.Find("table#searchResult tr").EachWithBreak(func(i int, s *goquery.Selection) bool {
		// Skip header row
		if i == 0 {
			return true
		}
		if len(results) >= limit {
			return false
		}

		// Column 1: name (td:eq(1) > a)
		nameTd := s.Find("td").Eq(1)
		nameLink := nameTd.Find("a").First()
		name := strings.TrimSpace(nameLink.Text())
		if name == "" {
			return true
		}

		// Column 3: magnet link
		magnetTd := s.Find("td").Eq(3)
		var magnet string
		magnetTd.Find("a[href^='magnet:']").Each(func(_ int, a *goquery.Selection) {
			if href, ok := a.Attr("href"); ok {
				magnet = href
			}
		})
		if magnet == "" {
			return true
		}

		ih, trackers, sanitized := parseMagnet(magnet)
		if ih == "" {
			return true
		}

		// Column 4: size
		sizeStr := normalizeSpace(s.Find("td").Eq(4).Text())
		size := parseTPBSize(sizeStr)

		// Column 2: date (format "01-02 2006" or "Today", "Yesterday")
		dateStr := normalizeSpace(s.Find("td").Eq(2).Text())
		added := parseTPBDate(dateStr)

		// Column 5: seeders
		seedsStr := strings.TrimSpace(s.Find("td").Eq(5).Text())
		seeds, _ := strconv.Atoi(seedsStr)

		// Column 6: leechers
		leechersStr := strings.TrimSpace(s.Find("td").Eq(6).Text())
		leechers, _ := strconv.Atoi(leechersStr)

		results = append(results, SearchResult{
			Provider:      t.Name(),
			Name:          name,
			InfoHash:      ih,
			Magnet:        sanitized,
			SizeBytes:     size,
			ReportedSeeds: seeds,
			ReportedPeers: leechers,
			Added:         added,
			Trackers:      trackers,
		})
		return true
	})

	if len(results) == 0 {
		return nil, fmt.Errorf("no results found on TPB for %q", query)
	}
	return results, nil
}

func (t *TPBSearch) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", tpbUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

var tpbSizeRE = regexp.MustCompile(`(?i)^\s*([0-9]*\.?[0-9]+)\s*(B|KiB|MiB|GiB|TiB)\s*$`)

func parseTPBSize(s string) int64 {
	m := tpbSizeRE.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	mult := int64(1)
	switch strings.ToUpper(m[2]) {
	case "KIB":
		mult = 1024
	case "MIB":
		mult = 1024 * 1024
	case "GIB":
		mult = 1024 * 1024 * 1024
	case "TIB":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(v * float64(mult))
}

// parseTPBDate handles TPB date formats: "07-20 2012", "Today", "Yesterday",
// and "MM-DD YYYY" or "Today HH:MM", "Yesterday HH:MM"
// normalizeSpace replaces non-breaking spaces with regular spaces and trims.
func normalizeSpace(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\xc2\xa0", " ")
	return strings.TrimSpace(s)
}

func parseTPBDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	now := time.Now()

	// "Today" or "Today HH:MM"
	if strings.HasPrefix(s, "Today") {
		parts := strings.Split(s, " ")
		if len(parts) >= 2 {
			t, err := time.Parse("15:04", parts[1])
			if err == nil {
				return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			}
		}
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}

	// "Yesterday" or "Yesterday HH:MM"
	if strings.HasPrefix(s, "Yesterday") {
		yesterday := now.AddDate(0, 0, -1)
		parts := strings.Split(s, " ")
		if len(parts) >= 2 {
			t, err := time.Parse("15:04", parts[1])
			if err == nil {
				return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			}
		}
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location())
	}

	// "MM-DD YYYY"
	t, err := time.Parse("01-02 2006", s)
	if err == nil {
		return t
	}

	return time.Time{}
}
