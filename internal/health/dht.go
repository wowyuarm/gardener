package health

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/anacrolix/dht/v2"
)

type DHT struct {
	server *dht.Server
}

func NewDHT() (*DHT, error) {
	cfg := dht.NewDefaultServerConfig()
	srv, err := dht.NewServer(cfg)
	if err != nil {
		return nil, fmt.Errorf("dht new server: %w", err)
	}
	return &DHT{server: srv}, nil
}

func (d *DHT) Close() {
	if d.server != nil {
		d.server.Close()
	}
}

// Bootstrap kicks off DHT routing table population. Safe to call once at startup.
func (d *DHT) Bootstrap(ctx context.Context) error {
	_, err := d.server.BootstrapContext(ctx)
	return err
}

// FindPeers performs a get_peers traversal for infoHash and returns unique peer addresses
// discovered before ctx is cancelled or the traversal completes.
func (d *DHT) FindPeers(ctx context.Context, infoHash string) ([]netip.AddrPort, error) {
	var ih [20]byte
	b, err := hex.DecodeString(infoHash)
	if err != nil || len(b) != 20 {
		return nil, errors.New("invalid infohash")
	}
	copy(ih[:], b)

	a, err := d.server.AnnounceTraversal(ih)
	if err != nil {
		return nil, fmt.Errorf("dht announce: %w", err)
	}
	defer a.Close()

	seen := make(map[netip.AddrPort]struct{})
	var mu sync.Mutex

	done := a.Finished()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-done:
			break loop
		case pv, ok := <-a.Peers:
			if !ok {
				break loop
			}
			mu.Lock()
			for _, p := range pv.Peers {
				addr, ok := netip.AddrFromSlice(p.IP)
				if !ok || p.Port <= 0 {
					continue
				}
				ap := netip.AddrPortFrom(addr.Unmap(), uint16(p.Port))
				seen[ap] = struct{}{}
			}
			mu.Unlock()
		}
	}

	out := make([]netip.AddrPort, 0, len(seen))
	for ap := range seen {
		out = append(out, ap)
	}
	return out, nil
}
