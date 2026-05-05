# gardener — agent usage guide

A CLI that takes a keyword, searches BT torrent indexes, and ranks results by
**verified** swarm health (peers that actually handshake and have all pieces).
Use this when the user wants to find a torrent that will actually download —
site-reported seeder counts are unreliable, gardener probes the swarm itself.

## When to invoke

- User asks to "find a torrent for X", "search BT for X", or wants the magnet
  link for a specific piece of media/software.
- User wants to verify a swarm is alive before downloading.
- Do **not** use for: looking up arbitrary URLs, downloading the actual
  content, or anything outside the search/verify scope.

## Invocation

```sh
gardener <query>                       # default: top 20 candidates, table output
gardener -n 30 <query>                 # test more candidates (slower)
gardener --json <query>                # machine-readable; parse this in scripts
gardener -v <query>                    # also include site-reported seed/peer counts
gardener --timeout 120s <query>        # extend overall wall-clock budget (default 90s)
```

The query may contain spaces; quote it or pass as multiple args.

### Flags

| flag                   | default       | meaning                                        |
|------------------------|---------------|------------------------------------------------|
| `-n, --limit`          | 20            | max candidates to verify                       |
| `--json`               | false         | emit JSON instead of table                     |
| `-v, --verbose`        | false         | add `rep_s/p` column with site-reported numbers|
| `--provider`           | torrentz2     | search backend (only `torrentz2` for now)      |
| `--timeout`            | 90s           | overall wall-clock budget                      |
| `--dht-timeout`        | 15s           | per-torrent DHT lookup window                  |
| `--tracker-timeout`    | 8s            | per-tracker scrape timeout                     |
| `--verify-timeout`     | 25s           | per-torrent handshake/metadata budget          |

## Reading the output

### Table mode (default)

```
| # | name | size | added | verified | dht | trk_avg | score | magnet |
```

- `verified` — peers that completed BT handshake **and** have every piece.
  This is the only field that proves a peer will serve you bytes right now.
- `dht` — unique peer addresses returned by DHT `get_peers`. Includes stale
  announces; treat as raw swarm size, not aliveness.
- `trk_avg` — mean seeder count across responsive trackers. Self-reported.
- `score` — weighted blend (verified × 10 dominates). Higher is better.
- `magnet` — minimal `magnet:?xt=urn:btih:<HASH>`. Sufficient on its own;
  any BT client will fill in trackers via DHT.

### Recommending results to the user

- **`verified > 0`**: the torrent is downloadable right now. Recommend the
  rank-1 result.
- **All `verified = 0` but some `dht > 0`**: swarm is dying. Stale DHT
  announces still exist but no peer in the sample completed a handshake.
  Tell the user the resource is likely dead, suggest re-running with a
  larger `--verify-timeout` or trying a different keyword.
- **All `verified = 0` and `dht = 0`**: dead. Don't recommend any.
- **Site-reported (`-v` shows `rep_s/p`) is high but `verified` is zero**:
  this is exactly what gardener is for. Trust `verified`, not the site.

### JSON mode

`--json` emits an array of objects, one per torrent, sorted by score desc.
Each object contains: `rank`, `name`, `infohash`, `magnet` (full, with
trackers), `size_bytes`, `added` (RFC3339), `reported_seeders`,
`reported_peers`, `dht_peers`, `tracker_seeders_avg`, `tracker_count`,
`tracker_ok`, `got_metadata`, `num_pieces`, `active_conns`,
`verified_seeders`, `partial_peers`, `score`.

For programmatic use, prefer JSON. Pipe to `jq` or read directly:

```sh
gardener --json "ubuntu 24.04" | jq '.[0] | {name, magnet, score, verified_seeders}'
```

## Wall-clock expectations

- Default run with `-n 20`: ~60–90 seconds, dominated by per-torrent
  metadata + settle window.
- `-n 5`: ~30–45 seconds.
- The progress log on stderr (`✓ name — verified=N dht=M ...`) prints as
  each torrent finishes; results table is rendered at the end after sort.

If the user is impatient, reduce `-n` rather than `--timeout`.

## Failure modes

- **No results from the index**: gardener exits non-zero with `no torrents
  found for "..."`. Try a different/broader query.
- **Network blocks UDP**: DHT degrades to zero peers. Tracker scrape and
  handshake (TCP) still work; rankings remain meaningful but compressed.
- **Stderr noise from anacrolix/torrent**: the underlying library prints
  some warnings about peer connections; these are not actionable.
- **Index HTML changed**: scraper may return zero candidates even for
  popular queries. Report to the user; the scraper needs updating.

## Output network requirements

gardener performs significant network activity:
- Outbound HTTPS to the index site (torrentz2.nz).
- Outbound UDP for DHT (port 6881-ish range, library-chosen).
- Outbound UDP/HTTP to listed trackers.
- Outbound TCP to peer addresses for handshake.

Sandboxes that block UDP or limit outbound TCP will produce degraded
results. Verify the network policy before blaming the tool.

## Do not

- Do not run gardener in a tight loop or with very large `-n` — each run
  joins the DHT and opens many sockets. Treat it as a one-shot per query.
- Do not pass the `magnet` column straight to a downloader without showing
  the user; respect that they may want to pick a non-rank-1 result.
- Do not use the table output for downstream parsing — use `--json`.
