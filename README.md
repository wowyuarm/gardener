# gardener

A CLI that searches BT torrent sites and ranks results by **verified** swarm health,
not by the seeder counts the site claims (which are usually stale or inflated).

## Why

`torrentz2.nz` and similar meta-search engines aggregate seeder counts that are
often wildly out of date. A torrent listed with "200 seeders" may have zero
peers actually serving pieces. `gardener` takes the top N candidates and:

1. Looks up live peers in the DHT.
2. Scrapes every tracker advertised in the magnet URI.
3. Connects to a sample of peers, fetches metadata via BEP-9, reads each peer's
   BITFIELD, and counts how many actually have all pieces.

The result is sorted by a weighted health score so the top entry is the one most
likely to actually download.

## Install

```sh
go install github.com/joway/gardener/cmd/gardener@latest
```

Or build locally:

```sh
go build -o gardener ./cmd/gardener
```

## Usage

```sh
gardener "ubuntu 24.04"
gardener -n 30 "..."                # test top 30 instead of default 20
gardener -v "..."                   # also show site-reported seeds/peers
gardener --json "..."               # machine-readable output with full magnets
gardener --timeout 120s "..."       # extend overall budget
```

## Output

Default table columns:

| column     | meaning                                                                  |
|------------|--------------------------------------------------------------------------|
| `#`        | rank by score (best first)                                               |
| `name`     | torrent name                                                             |
| `size`     | total content size                                                       |
| `added`    | age (e.g. `3y`, `2mo`)                                                   |
| `verified` | peers that handshook AND have every piece — this is the gold signal      |
| `dht`      | unique peer addresses returned by DHT `get_peers`                        |
| `trk_avg`  | average seeder count across responsive trackers                          |
| `score`    | weighted health score (see below)                                        |
| `magnet`   | minimal `magnet:?xt=urn:btih:<HASH>` — paste into any BT client          |

`--json` adds: full magnet (with `dn` + all `tr=`), per-tracker results,
metadata acquisition status, total piece count, partial-vs-seeder breakdown.

## Scoring

```
score = 10 * verified_seeders        # peers we actually handshook + bitfield-confirmed
      +  2 * dht_peers                # raw DHT swarm size
      +  1 * tracker_seeders_avg      # what trackers report
      +  0.5 * age_factor             # 1 / (1 + years), favors newer entries on ties
```

`verified_seeders` dominates by design: it is the only signal that proves a peer
will actually serve bytes to you right now. The other terms break ties when
verification didn't sample enough of the swarm in time.

A score of 0 with `dht > 0` means DHT still has stale announces but no peer in
the sample completed a handshake — usually a dying swarm.

## How verification works

For each candidate torrent, in parallel:

- **DHT phase** — `AnnounceTraversal` against a shared, bootstrapped DHT server
  collects peer addresses for ~15s.
- **Tracker phase** — UDP/HTTP scrape against every tracker URL in the magnet.
- **Handshake phase** — feeds DHT-discovered peers into a shared `torrent.Client`,
  waits for metadata via BEP-9, then samples up to 30 peer connections. A peer is
  counted as `verified_seeders` when its `PeerPieces()` bitmap covers all pieces.

The whole pipeline is wall-clock bounded (`--timeout`, default 90s).

## Adding a search provider

Implement `internal/search.Provider`:

```go
type Provider interface {
    Name() string
    Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}
```

Wire it up in `cmd/gardener/main.go:pickProvider`. `SearchResult.Magnet` must be
sanitized — only `udp/http/https` trackers in the `tr=` params (anacrolix/torrent
panics on unknown schemes). See `parseMagnet` in `internal/search/torrentz2.go`
for the reference implementation.

## Caveats

- DHT bootstrap is best-effort; if your network blocks UDP, scoring degrades to
  trackers + handshake only.
- Handshake sampling is capped at 30 peers per torrent. Healthy-but-slow-to-respond
  swarms may show `verified < dht`. Raise `--verify-timeout` if that hurts you.
- torrentz2 is HTML-scraped; if the site changes its markup, the scraper breaks.
- The verifier writes to a temp dir but never actually downloads piece data. The
  dir is cleaned up on exit.
