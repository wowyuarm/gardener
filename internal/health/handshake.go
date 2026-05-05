package health

import (
	"context"
	"net/netip"
	"os"
	"time"

	analog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
)

const (
	maxSampledPeers      = 30
	maxEstablishedConns  = 30
	postMetaSettleWindow = 8 * time.Second
)

// VerifyResult holds the outcome of a handshake-based verification pass for one torrent.
type VerifyResult struct {
	GotMetadata    bool
	NumPieces      int
	ActiveConns    int // peers that completed handshake and have a connection alive
	VerifiedSeeds  int // peers whose bitfield indicates they have every piece
	PartialPeers   int // peers connected but not seeders
	MetadataPeerID string
}

// Verifier wraps a shared torrent.Client used to handshake and pull metadata for many torrents.
type Verifier struct {
	cl      *torrent.Client
	dataDir string
}

func NewVerifier() (*Verifier, error) {
	dataDir, err := os.MkdirTemp("", "gardener-bt-*")
	if err != nil {
		return nil, err
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.DefaultStorage = storage.NewFile(dataDir)
	cfg.NoUpload = true
	cfg.Seed = false
	cfg.NoDefaultPortForwarding = true
	cfg.DisableAcceptRateLimiting = true
	// Pick a free ephemeral port so concurrent gardener runs (or other anacrolix
	// clients) don't clash on the default 42069.
	cfg.ListenPort = 0
	cfg.Logger = cfg.Logger.WithFilterLevel(analog.Disabled)
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		os.RemoveAll(dataDir)
		return nil, err
	}
	return &Verifier{cl: cl, dataDir: dataDir}, nil
}

func (v *Verifier) Close() {
	if v.cl != nil {
		v.cl.Close()
	}
	if v.dataDir != "" {
		os.RemoveAll(v.dataDir)
	}
}

// Verify adds the magnet, optionally injects DHT-discovered peers, waits for metadata,
// then samples peer connections to count seeders by bitfield. Bounded by ctx.
func (v *Verifier) Verify(ctx context.Context, magnet string, dhtPeers []netip.AddrPort) VerifyResult {
	var res VerifyResult

	t, err := v.cl.AddMagnet(magnet)
	if err != nil {
		return res
	}
	defer t.Drop()

	t.SetMaxEstablishedConns(maxEstablishedConns)

	if len(dhtPeers) > 0 {
		seeded := dhtPeers
		if len(seeded) > maxSampledPeers {
			seeded = seeded[:maxSampledPeers]
		}
		peers := make([]torrent.PeerInfo, 0, len(seeded))
		for _, ap := range seeded {
			peers = append(peers, torrent.PeerInfo{
				Addr:   peerAddr(ap),
				Source: torrent.PeerSourceDhtGetPeers,
			})
		}
		t.AddPeers(peers)
	}

	select {
	case <-t.GotInfo():
		res.GotMetadata = true
	case <-ctx.Done():
		return res
	}

	info := t.Info()
	if info != nil {
		res.NumPieces = info.NumPieces()
	}

	settle, cancel := context.WithTimeout(ctx, postMetaSettleWindow)
	defer cancel()
	<-settle.Done()

	conns := t.PeerConns()
	for _, pc := range conns {
		bm := pc.PeerPieces()
		if bm == nil {
			continue
		}
		card := int(bm.GetCardinality())
		if card == 0 {
			continue
		}
		res.ActiveConns++
		if res.NumPieces > 0 && card >= res.NumPieces {
			res.VerifiedSeeds++
		} else {
			res.PartialPeers++
		}
	}
	return res
}

// peerAddr satisfies torrent.PeerRemoteAddr (which is just net.Addr).
type peerAddr netip.AddrPort

func (p peerAddr) Network() string { return "tcp" }
func (p peerAddr) String() string  { return netip.AddrPort(p).String() }
