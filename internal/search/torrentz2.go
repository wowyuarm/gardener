package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	torrentz2Base      = "https://torrentz2.nz"
	torrentz2UA        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	detailFetchWorkers = 8
)

type Torrentz2 struct {
	client *http.Client
}

func NewTorrentz2() *Torrentz2 {
	return &Torrentz2{client: &http.Client{Timeout: 20 * time.Second}}
}

func (t *Torrentz2) Name() string { return "torrentz2" }

func (t *Torrentz2) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	u := fmt.Sprintf("%s/search?q=%s&sort=seeders", torrentz2Base, url.QueryEscape(query))
	doc, err := t.fetchDoc(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch search: %w", err)
	}

	type stub struct {
		id            string
		name          string
		size          int64
		reportedSeeds int
		reportedPeers int
		added         time.Time
	}
	var stubs []stub
	doc.Find("div.results > dl").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if len(stubs) >= limit {
			return false
		}
		link := s.Find("dt > a").First()
		href, _ := link.Attr("href")
		id := strings.TrimPrefix(href, "/torrent/")
		if id == "" || id == href {
			return true
		}
		name := strings.TrimSpace(link.Text())
		size := parseSize(s.Find("dd > span.s").First().Text())
		seeds, _ := strconv.Atoi(strings.TrimSpace(s.Find("dd > span.u").First().Text()))
		peers, _ := strconv.Atoi(strings.TrimSpace(s.Find("dd > span.d").First().Text()))
		var added time.Time
		if title, ok := s.Find("dd > span.a > span").First().Attr("title"); ok {
			added = parseAddedTime(title)
		}
		stubs = append(stubs, stub{id: id, name: name, size: size, reportedSeeds: seeds, reportedPeers: peers, added: added})
		return true
	})

	if len(stubs) == 0 {
		return nil, errors.New("no results parsed from torrentz2")
	}

	results := make([]SearchResult, len(stubs))
	sem := make(chan struct{}, detailFetchWorkers)
	var wg sync.WaitGroup
	for i, st := range stubs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, st stub) {
			defer wg.Done()
			defer func() { <-sem }()
			magnet, err := t.fetchMagnet(ctx, st.id)
			if err != nil {
				return
			}
			ih, trackers, sanitized := parseMagnet(magnet)
			results[i] = SearchResult{
				Provider:      t.Name(),
				Name:          st.name,
				InfoHash:      ih,
				Magnet:        sanitized,
				SizeBytes:     st.size,
				ReportedSeeds: st.reportedSeeds,
				ReportedPeers: st.reportedPeers,
				Added:         st.added,
				Trackers:      trackers,
			}
		}(i, st)
	}
	wg.Wait()

	out := results[:0]
	for _, r := range results {
		if r.InfoHash != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (t *Torrentz2) fetchDoc(ctx context.Context, u string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", torrentz2UA)
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

func (t *Torrentz2) fetchMagnet(ctx context.Context, id string) (string, error) {
	doc, err := t.fetchDoc(ctx, fmt.Sprintf("%s/torrent/%s", torrentz2Base, id))
	if err != nil {
		return "", err
	}
	var magnet string
	doc.Find("a[href^='magnet:']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if href, ok := s.Attr("href"); ok {
			magnet = href
			return false
		}
		return true
	})
	if magnet == "" {
		return "", errors.New("magnet not found")
	}
	return magnet, nil
}

var sizeRE = regexp.MustCompile(`(?i)^\s*([0-9]*\.?[0-9]+)\s*(B|KB|MB|GB|TB)\s*$`)

func parseSize(s string) int64 {
	m := sizeRE.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	mult := int64(1)
	switch strings.ToUpper(m[2]) {
	case "KB":
		mult = 1024
	case "MB":
		mult = 1024 * 1024
	case "GB":
		mult = 1024 * 1024 * 1024
	case "TB":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(v * float64(mult))
}

// torrentz2 emits dates like "Thu Apr 18 2019 17:11:32 GMT+0000 (Coordinated Universal Time)"
var addedLayouts = []string{
	"Mon Jan 02 2006 15:04:05 GMT-0700",
	"Mon Jan 2 2006 15:04:05 GMT-0700",
}

func parseAddedTime(s string) time.Time {
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	for _, layout := range addedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseMagnet extracts the infohash and trackers from a magnet URI, and returns a
// sanitized magnet URI containing only trackers with schemes anacrolix/torrent can
// safely consume (udp/http/https). This avoids a panic in the torrent client's
// background tracker dispatcher when it encounters wss:// or other unknown schemes.
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
