---
name: gardener
description: Search BT torrents and rank results by verified swarm health. Probes DHT, trackers, and BT handshake to confirm peers actually have complete data. Use when user wants to find torrents, verify swarms, or get magnet links for media/software.
---

# Gardener

Search BT torrents and verify swarm health before downloading.

## Setup

Build from source: `go build -o gardener ./cmd/gardener` (or `go install`).
Requires Go 1.25+. `aria2c` recommended for downloads.

## Providers

| provider | scope | status |
|----------|-------|--------|
| `tpb` | Movies, TV, software, general | ✅ The Pirate Bay via pirateproxy.live |
| `nyaa` | Anime, Asian media | ✅ Nyaa.si |
| `torrentz2` | General meta-search | ⚠️ often blocked by CDN/firewall |

Default is `torrentz2`. Always use `--provider tpb` or `--provider nyaa` explicitly.

## Commands

```bash
gardener --provider tpb "memento 2000"        # search TPB
gardener --provider nyaa "shingeki no kyojin"  # search Nyaa
gardener -n 5 "query"                         # fewer candidates, faster
gardener --json "query"                       # machine-readable output
gardener -v "query"                           # show site-reported seed/peer
gardener -d "query"                           # auto-download best result with aria2c
gardener --timeout 120s "query"               # extend overall budget
```

### All flags

| flag | default | meaning |
|------|---------|---------|
| `-n, --limit` | 20 | max candidates to verify |
| `--json` | false | JSON output |
| `-v, --verbose` | false | show site-reported seed/peer columns |
| `-d, --download` | false | auto-download best result with aria2c |
| `--provider` | torrentz2 | search backend: tpb, nyaa, torrentz2 |
| `--timeout` | 90s | overall wall-clock budget |
| `--dht-timeout` | 15s | per-torrent DHT lookup window |
| `--tracker-timeout` | 8s | per-tracker scrape timeout |
| `--verify-timeout` | 25s | per-torrent handshake/metadata budget |

## Reading output

### Table columns

| column | meaning |
|--------|---------|
| `verified` | Peers handshook + have all pieces. Gold signal. |
| `dht` | Unique peers from DHT. Includes stale announces. |
| `trk_avg` | Average seeder count from responsive trackers. |
| `score` | Weighted blend (verified×10 + dht×2 + tracker×1 + age×0.5) |

Results sorted by **verified seeders first**, then score as tiebreaker.

### Recommending to user

- `verified > 0`: downloadable now. Recommend rank-1 result.
- All `verified = 0` but `dht > 0`: dying swarm. Retry with longer `--verify-timeout` or different query.
- All `verified = 0` and `dht = 0`: dead. Don't recommend.
- Site-reported high but `verified` zero: classic case. Trust `verified`.

### JSON mode (`--json`)

Each object: `rank`, `name`, `infohash`, `magnet` (full), `size_bytes`, `added`, `reported_seeders`, `reported_peers`, `dht_peers`, `tracker_seeders_avg`, `tracker_count`, `tracker_ok`, `got_metadata`, `num_pieces`, `active_conns`, `verified_seeders`, `partial_peers`, `score`.

## How verification works

For each candidate torrent, in parallel (up to 4 at a time):

1. **DHT phase** — `AnnounceTraversal` against a bootstrapped DHT server collects peer addresses
2. **Tracker phase** — UDP/HTTP scrape against every tracker URL in the magnet
3. **Handshake phase** — feeds DHT-discovered peers into a `torrent.Client`, waits for metadata via BEP-9, then samples up to 30 peer connections. A peer is `verified_seeders` when its `PeerPieces()` bitmap covers all pieces.

## Limitations

- Each run takes 30-90s (DHT bootstrap + per-torrent verification)
- UDP required for DHT; if blocked, `dht` column will be 0
- Sites are HTML-scraped; markup changes can break scraping
- torrentz2 often blocked; use `--provider tpb` or `--provider nyaa` as fallback
- Nyaa best for Asian content; use `tpb` for Western movies/TV
- Handshake sampling capped at 30 peers per torrent
- `--download` requires `aria2c` in PATH

## Do not

- Run in tight loop or with very large `-n` (many sockets)
- Pass magnet to downloader without showing user first
- Parse table output programmatically (use `--json`)
