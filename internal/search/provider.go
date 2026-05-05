package search

import (
	"context"
	"time"
)

type SearchResult struct {
	Provider      string
	Name          string
	InfoHash      string
	Magnet        string
	SizeBytes     int64
	ReportedSeeds int
	ReportedPeers int
	Added         time.Time
	Trackers      []string
}

type Provider interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}
