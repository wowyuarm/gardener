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
	nyaaBase     = "https://nyaa.si"
	nyaaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Nyaa struct {
	client *http.Client
}

func NewNyaa() *Nyaa {
	return &Nyaa{client: &http.Client{Timeout: 20 * time.Second}}
}

func (n *Nyaa) Name() string { return "nyaa" }

func (n *Nyaa) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	// Sort by seeders desc: &s=seeders&o=desc
	u := fmt.Sprintf("%s/?f=0&c=0_0&q=%s&s=seeders&o=desc", nyaaBase, url.QueryEscape(query))
	doc, err := n.fetchDoc(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch search: %w", err)
	}

	var results []SearchResult
	doc.Find("table tbody tr").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if len(results) >= limit {
			return false
		}

		// Column 0: category (skip)
		// Column 1+2: name (colspan=2)
		nameTd := s.Find("td").Eq(1)
		nameLink := nameTd.Find("a").First()
		name := strings.TrimSpace(nameLink.Text())
		if name == "" {
			return true
		}

		// Column 2 (actually 3rd td): download + magnet links
		linkTd := s.Find("td").Eq(2)
		var magnet string
		linkTd.Find("a[href^='magnet:']").Each(func(_ int, a *goquery.Selection) {
			if href, ok := a.Attr("href"); ok {
				magnet = href
			}
		})
		if magnet == "" {
			return true
		}

		// Parse magnet
		ih, trackers, sanitized := parseMagnet(magnet)
		if ih == "" {
			return true
		}

		// Column 3: size
		sizeStr := strings.TrimSpace(s.Find("td").Eq(3).Text())
		size := parseNyaaSize(sizeStr)

		// Column 4: date (has data-timestamp)
		dateTd := s.Find("td").Eq(4)
		tsStr, hasTs := dateTd.Attr("data-timestamp")
		var added time.Time
		if hasTs {
			if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
				added = time.Unix(ts, 0)
			}
		}

		// Column 5: seeders
		seedsStr := strings.TrimSpace(s.Find("td").Eq(5).Text())
		seeds, _ := strconv.Atoi(seedsStr)

		// Column 6: leechers
		leechersStr := strings.TrimSpace(s.Find("td").Eq(6).Text())
		leechers, _ := strconv.Atoi(leechersStr)

		results = append(results, SearchResult{
			Provider:      n.Name(),
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
		return nil, fmt.Errorf("no results found on nyaa for %q", query)
	}
	return results, nil
}

func (n *Nyaa) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", nyaaUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// parseNyaaSize handles sizes like "624.0 MiB", "1.5 GiB", "7.2 KiB"
var nyaaSizeRE = regexp.MustCompile(`(?i)^\s*([0-9]*\.?[0-9]+)\s*(KiB|MiB|GiB|TiB|B)\s*$`)

func parseNyaaSize(s string) int64 {
	m := nyaaSizeRE.FindStringSubmatch(s)
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
