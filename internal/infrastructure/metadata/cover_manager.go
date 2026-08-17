package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

// CoverManager owns the cover-image lifecycle: extraction of embedded cover
// art to disk, the images-table rows (original + variants), and cleanup.
//
// Layout conventions:
//   - track originals: {images}/{library}/track_{id}.jpg
//   - track thumbnails: {images}/{library}/track_{id}_64.jpg
//   - album originals reference a track's original path (no copy)
//   - album thumbnails: {images}/album/album_{id}_256.jpg
//
// The 64/256 variant is only produced when the source is larger than the
// target; otherwise the variant references the original path, so a missing
// thumbnail file is a normal state, not a missing cover.
type CoverManager struct {
	imagesDir string
	extractor *CoverExtractor
	images    *repository.ImageRepo
	albums    *repository.AlbumRepo
	tracks    *repository.TrackRepo

	// newRegistry builds the current metadata source chain (settings-aware)
	// when the embedded cover is missing and a platform cover should be
	// looked up through the track's source. Nil disables network covers.
	newRegistry func() *Registry

	// extractMu serializes re-extraction so concurrent cover requests for
	// the same missing file cannot race on file writes and row replacement.
	extractMu sync.Mutex

	// recentFails memoizes failed extractions (keyed by track/album id) so a
	// broken track (e.g. its audio file was deleted) is not re-attempted by
	// spawning ffmpeg on every cover request.
	recentFails map[string]time.Time
	failMu      sync.Mutex
}

const coverFailCooldown = 60 * time.Second

// maxNetworkCoverBytes caps downloaded cover payloads (e.g. NetEase picUrl)
// so a hostile or misconfigured upstream cannot exhaust memory.
const maxNetworkCoverBytes = 20 << 20

// maxCoverDimension caps the pixel size of downloaded covers (checked via
// the image header before a full decode) so a small payload that declares a
// huge canvas cannot blow up memory during decode/resize.
const maxCoverDimension = 4096

// maxCoverCandidateAttempts bounds how many platform cover candidates are
// downloaded before giving up (each fetch can take up to 30s), so a slow or
// hung CDN cannot stall the whole scan for minutes.
const maxCoverCandidateAttempts = 2

// vettedHTTPClient is the shared client for downloaded covers. Reusing one
// Transport (instead of a fresh one per fetch) avoids leaking an idle
// connection + readLoop/writeLoop goroutine per download: a transport whose
// IdleConnTimeout is zero never reaps idle connections, and nothing here
// calls CloseIdleConnections.
var vettedHTTPClient = sync.OnceValue(func() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips := dialIPs(ctx, host)
				if len(ips) == 0 {
					return nil, fmt.Errorf("cover host %q resolves to no public address", host)
				}
				// Try every public address (CDNs commonly return several
				// A/AAAA records); a dead first hop must not fail the
				// download when another address works.
				var lastErr error
				for _, ip := range ips {
					conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if derr == nil {
						return conn, nil
					}
					lastErr = derr
				}
				return nil, lastErr
			},
			IdleConnTimeout: 90 * time.Second,
		},
		CheckRedirect: redirectGuard,
	}
})

// dialIPs resolves host to the set of public addresses suitable for the
// pinned dialer. A literal IP is validated directly; otherwise the host is
// resolved and every non-public address is dropped. The resolution is bounded
// by a 10s timeout and inherits the caller's cancellation so a cancelled
// request (client disconnect, server shutdown) releases the goroutine instead
// of lingering on a stuck resolver.
func dialIPs(ctx context.Context, host string) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		if publicIP(ip) {
			return []net.IP{ip}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		if publicIP(a.IP) {
			out = append(out, a.IP)
		}
	}
	return out
}

func vettedClient() *http.Client { return vettedHTTPClient() }

// redirectGuard constrains cover redirects: a bounded hop count, no https→http
// downgrade from the original request, and every hop host re-vetted.
func redirectGuard(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("https→http redirect downgrade blocked")
	}
	if !safeCoverURL(req.URL.String()) {
		return errors.New("redirect to disallowed cover host")
	}
	return nil
}

func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// Non-global unicast ranges the stdlib predicate misses — CGNAT, the
	// TEST-NET documentation blocks and the reserved 240.0.0.0/4. They are
	// not globally routable, so treating them as reachable would widen the
	// SSRF white-list into carrier-grade or reserved space.
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, p := range nonGlobalNetworks {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// nonGlobalNetworks lists non-global unicast blocks excluded by publicIP in
// addition to net.IP's built-in loopback/private/link-local/unspecified/
// multicast predicates. 0.0.0.0/8 is the important one: Linux routes it
// locally, so a host like 0.0.0.1 bypasses the IsLoopback/IsUnspecified
// checks and hits the local machine.
var nonGlobalNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	// IPv6 translation/mapping prefixes that can carry or map to arbitrary
	// IPv4 targets (e.g. 2002:7f00:1:: → 127.0.0.1, 64:ff9b::a00:1 →
	// 10.0.0.1); net.IP's predicates do not cover them.
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	// RFC 8215 NAT64 local-use prefix (distinct from 64:ff9b::/96; maps
	// private IPv4, e.g. 64:ff9b:1::a00:1 → 10.0.0.1).
	netip.MustParsePrefix("64:ff9b:1::/48"),
	// Deprecated special-use blocks kept closed for completeness: ::/96
	// (IPv4-compatible), fec0::/10 (site-local, RFC 3879) and the 6to4
	// relay anycast 192.88.99.0/24 (RFC 7526).
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("192.88.99.0/24"),
}

// fetchImage downloads a cover image and validates that the payload decodes
// as an image. A 30s timeout bounds a hung upstream; request cancellation is
// propagated through the context. The URL is validated (scheme + host) and
// connections are pinned to public addresses at dial time, so a compromised
// upstream cannot pivot the server into internal services.
func fetchImage(ctx context.Context, url string) ([]byte, error) {
	if !safeCoverURL(url) {
		return nil, fmt.Errorf("cover request: rejected URL %q", url)
	}
	return fetchImageWithClient(ctx, url, vettedClient())
}

// fetchImageUnchecked downloads and validates an image payload without the
// SSRF host guard (tests use loopback servers). Callers must have already
// vetted the URL via safeCoverURL.
func fetchImageUnchecked(ctx context.Context, url string) ([]byte, error) {
	return fetchImageWithClient(ctx, url, nil)
}

// fetchImageWithClient performs the shared download pipeline (redirect guard,
// size cap, dimension cap, decode validation) with the given client; a nil
// client uses a plain loopback-friendly one.
func fetchImageWithClient(ctx context.Context, url string, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cover request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if client == nil {
		client = &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: redirectGuard,
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cover download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover download: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxNetworkCoverBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cover read: %w", err)
	}
	if len(data) > maxNetworkCoverBytes {
		return nil, fmt.Errorf("cover download: payload exceeds %d bytes", maxNetworkCoverBytes)
	}
	// Check the canvas size from the header before a full decode so a tiny
	// payload declaring a huge image cannot exhaust memory.
	cfg, format, cerr := image.DecodeConfig(bytes.NewReader(data))
	if cerr != nil {
		return nil, fmt.Errorf("cover download: not a decodable image: %w", cerr)
	} else if cfg.Width > maxCoverDimension || cfg.Height > maxCoverDimension {
		return nil, fmt.Errorf("cover download: image %dx%d exceeds %dpx limit", cfg.Width, cfg.Height, maxCoverDimension)
	}
	// Full-decode validate only JPEG payloads: non-JPEG covers are re-encoded
	// by ensureJPEG right after this, which full-decodes them anyway, so a
	// second decode here would double the peak memory of large covers.
	if format == "jpeg" {
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("cover download: not a decodable image: %w", err)
		}
	}
	return data, nil
}

// safeCoverURL reports whether a cover URL is acceptable: http/https only,
// with a host resolving to public addresses (no loopback, private, link-local
// or unspecified IPs) — so a malicious picUrl cannot pivot the server into
// internal services (cloud metadata 169.254.169.254, management ports, ...).
// The check is a cheap pre-filter; the vetted dialer re-resolves and pins at
// connect time to close the DNS-rebinding window.
func safeCoverURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		return publicIP(ip)
	}
	// Bound the resolution explicitly: this pre-filter runs before the HTTP
	// client's timeout starts, and every redirect hop re-runs it, so a hung
	// resolver must not block a goroutine indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := (&net.Resolver{}).LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return false
	}
	// A resolvable name with no address records is not acceptable either: an
	// empty list must not silently pass the pre-filter.
	if len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !publicIP(a.IP) {
			return false
		}
	}
	return true
}

func NewCoverManager(imagesDir string, db *sql.DB, newRegistry func() *Registry) *CoverManager {
	return &CoverManager{
		imagesDir:   imagesDir,
		extractor:   NewCoverExtractor(imagesDir),
		images:      repository.NewImageRepo(db),
		albums:      repository.NewAlbumRepo(db),
		tracks:      repository.NewTrackRepo(db),
		newRegistry: newRegistry,
		recentFails: make(map[string]time.Time),
	}
}

func (m *CoverManager) recentFail(key string) bool {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	if t, ok := m.recentFails[key]; ok && time.Since(t) < coverFailCooldown {
		return true
	}
	return false
}

func (m *CoverManager) noteFail(key string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	// Opportunistically purge expired entries so the map stays bounded over
	// the server's lifetime.
	for k, t := range m.recentFails {
		if time.Since(t) >= coverFailCooldown {
			delete(m.recentFails, k)
		}
	}
	m.recentFails[key] = time.Now()
}

func (m *CoverManager) clearFail(key string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	delete(m.recentFails, key)
}

// TrackCoverComplete reports whether the track's cover is present. In
// non-overwrite mode the DB pointer alone is authoritative; in overwrite
// mode the stored original must also exist on disk. The original path is
// derived from the fixed layout, avoiding an images-row lookup per track
// during overwrite scans.
func (m *CoverManager) TrackCoverComplete(ctx context.Context, track *domain.Track, overwrite bool) bool {
	if track == nil || track.CoverImageID == nil {
		return false
	}
	if !overwrite {
		return true
	}
	p := CoverPath(m.imagesDir, track.LibraryID, "track", track.ID, "jpg")
	return CoverFileExists(p)
}

// AlbumCoverComplete mirrors TrackCoverComplete for albums.
func (m *CoverManager) AlbumCoverComplete(ctx context.Context, album *domain.Album, overwrite bool) bool {
	if album == nil || album.CoverImageID == nil {
		return false
	}
	if !overwrite {
		return true
	}
	img, err := m.images.FindByID(ctx, *album.CoverImageID)
	if err != nil {
		return false
	}
	return CoverFileExists(img.Path)
}

// ExtractTrackCover extracts the embedded cover of a track, writes the
// original (and 64px thumbnail when the source is larger), records the
// images row, persists the track, and backfills the album cover from this
// track when the album has none.
//
// Files are written under temporary names and renamed only after the new
// images row exists, so a failure at any step leaves the previous row and
// files intact.
func (m *CoverManager) ExtractTrackCover(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, force bool) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	// The failure memo only applies to on-demand HTTP restores; scans pass
	// force=true so a previously failing track is retried.
	if !force && m.recentFail(track.ID) {
		return fmt.Errorf("cover extraction recently failed for %s", track.ID)
	}
	// Concurrent requests for the same missing cover serialize on the lock;
	// if a previous request already restored every file (original and
	// variants), nothing to do.
	if !force && track.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && imageFilesIntact(img) {
			return nil
		}
	}

	data, _, err := m.extractor.ExtractFromFile(ctx, track.FilePath)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}
	// Originals are stored as JPEG regardless of the embedded format.
	data, err = ensureJPEG(data)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}
	return m.importTrackCoverLocked(ctx, libraryID, track, album, data, "embedded")
}

// EnsureTrackCover makes sure the track has a cover, following the unified
// flow used by scans and on-demand HTTP restores: the embedded cover is
// extracted first; when the file carries none and searchPlatform is true,
// the platform chain is asked for the track (the query is built from its own
// title/album, so the source recorded in metadata_source re-provides the
// cover) and the hit's cover is downloaded and imported with the "network"
// source. Both paths land in the same hash-split / atomic-commit pipeline.
//
// force bypasses the failure memo and the intact re-check (scans); false
// applies them (HTTP requests). searchPlatform enables the platform lookup:
// callers that already ran the recognition chain and got no match pass
// false so the search is not repeated (scanner/reidentify); on-demand HTTP
// restores pass true (no recognition context available). Failures of either
// stage are memoized for the cooldown window.
func (m *CoverManager) EnsureTrackCover(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, force, searchPlatform bool) error {
	// ---- Embedded extraction runs under the lock (short critical section) ----
	m.extractMu.Lock()
	if !force && m.recentFail(track.ID) {
		m.extractMu.Unlock()
		return fmt.Errorf("cover extraction recently failed for %s", track.ID)
	}
	// Concurrent requests for the same missing cover serialize on the lock;
	// if a previous request already restored every file (original and
	// variants), nothing to do.
	if !force && track.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && imageFilesIntact(img) {
			m.extractMu.Unlock()
			return nil
		}
	}

	data, _, err := m.extractor.ExtractFromFile(ctx, track.FilePath)
	if err == nil {
		data, err = ensureJPEG(data)
		if err == nil {
			err = m.importTrackCoverLocked(ctx, libraryID, track, album, data, "embedded")
			if err == nil {
				m.clearFail(track.ID)
				m.extractMu.Unlock()
				return nil
			}
		}
	}
	m.extractMu.Unlock()

	// ---- Platform cover lookup + download runs OUTSIDE the lock so a slow
	// or hung upstream/DNS cannot serialize every cover operation globally. ----
	var platformErr error
	var networkData []byte
	if searchPlatform && m.newRegistry != nil && track.Title != "" {
		q := port.MetadataQuery{Title: track.Title}
		if album != nil && album.Title != "" {
			q.Album = album.Title
		}
		candidates, rerr := m.newRegistry().SearchCandidates(ctx, q)
		if rerr == nil {
			// Bound how many downloads a slow/hung upstream can stall the
			// whole scan: a single candidate download can take up to 30s, so
			// at most the first few plausible hits are attempted.
			attempts := 0
			for _, c := range candidates {
				// The cover URL is only trusted when the candidate clears
				// the source's confidence threshold; otherwise unrelated
				// search hits (e.g. any track carrying a picUrl) would
				// attach a random image to an unidentified song.
				if c.CoverArtURL == "" || c.Score < identifyThreshold {
					continue
				}
				if attempts >= maxCoverCandidateAttempts {
					break
				}
				attempts++
				d, derr := fetchImage(ctx, c.CoverArtURL)
				if derr != nil {
					err = derr
					continue
				}
				d, derr = ensureJPEG(d)
				if derr != nil {
					err = derr
					continue
				}
				networkData = d
				break
			}
		} else {
			// Surface the platform failure so callers can distinguish a
			// source outage from a genuinely coverless track.
			platformErr = rerr
			log.Printf("[cover] platform lookup error for %s: %v", track.ID, rerr)
		}
	}

	// ---- Re-acquire the lock; a concurrent request may have restored the
	// cover while we were downloading. Query the CURRENT database state by
	// owner: the caller's track pointer is a stale snapshot and cannot see a
	// cover a concurrent request imported during the download window.
	m.extractMu.Lock()
	defer m.extractMu.Unlock()
	if !force {
		if img, err := m.images.FindByOwner(ctx, "track", track.ID); err == nil && imageFilesIntact(img) {
			return nil
		}
	}
	if networkData != nil {
		if ierr := m.importTrackCoverLocked(ctx, libraryID, track, album, networkData, "network"); ierr == nil {
			m.clearFail(track.ID)
			return nil
		} else {
			err = ierr
		}
	}

	// Surface the platform failure alongside whatever embedded-extraction
	// error we already carry so callers can distinguish a source outage from
	// a genuinely coverless track.
	if err == nil {
		if platformErr != nil {
			err = fmt.Errorf("no embedded cover and platform lookup failed for %s: %w", track.ID, platformErr)
		} else {
			err = fmt.Errorf("no embedded cover and no platform cover for %s", track.ID)
		}
	} else if platformErr != nil {
		// %w keeps the embedded-extraction error in the chain so callers can
		// still inspect the original root cause (e.g. a genuinely coverless
		// file vs an upstream fault) via errors.Is/errors.As.
		err = fmt.Errorf("%w; platform lookup failed: %w", err, platformErr)
	}
	m.noteFail(track.ID)
	return err
}

// ImportTrackCoverURL downloads a cover from a platform-provided URL and
// imports it as the track's cover with the "network" source, through the
// same hash-split / atomic-commit pipeline as embedded extraction. Used by
// the scanner to reuse the cover URL the recognition chain already resolved
// (its search runs on the original file tags, which may differ from the
// stored title after a user edit). The download runs outside the lock so a
// slow upstream cannot stall other covers; failures are memoized for the
// cooldown window.
func (m *CoverManager) ImportTrackCoverURL(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, coverURL string) error {
	if m.recentFail(track.ID) {
		return fmt.Errorf("cover import recently failed for %s", track.ID)
	}
	m.extractMu.Lock()
	if track.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && imageFilesIntact(img) {
			m.extractMu.Unlock()
			return nil
		}
	}
	m.extractMu.Unlock()

	data, err := fetchImage(ctx, coverURL)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}
	data, err = ensureJPEG(data)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}

	m.extractMu.Lock()
	defer m.extractMu.Unlock()
	// Re-check the current DB state by owner: a concurrent request may have
	// imported the cover during the download window, which the caller's stale
	// track pointer cannot see.
	if img, err := m.images.FindByOwner(ctx, "track", track.ID); err == nil && imageFilesIntact(img) {
		return nil
	}
	if err := m.importTrackCoverLocked(ctx, libraryID, track, album, data, "network"); err != nil {
		m.noteFail(track.ID)
		return err
	}
	m.clearFail(track.ID)
	return nil
}

// importTrackCoverLocked runs the shared import pipeline: staging files
// under temporary names, the hash-split (unchanged content restores files
// only and keeps the image id stable; changed content rebuilds the row with
// create → repoint → rename → retire ordering), and the album cover
// backfill when the album has none. Caller must hold extractMu and pass
// JPEG-encoded cover bytes.
func (m *CoverManager) importTrackCoverLocked(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, data []byte, source string) error {

	thumbDir := filepath.Join(m.imagesDir, libraryID)
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		m.noteFail(track.ID)
		return err
	}
	mainPath := CoverPath(m.imagesDir, libraryID, "track", track.ID, "jpg")
	thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", track.ID))
	format, w, h, hash := imageInfo(data)

	// Content unchanged (a plain restore of a deleted file): the existing
	// images row and cover_image_id stay untouched, only the files are
	// written back. Content changed (replaced file / overwrite scan) or a
	// missing row: rebuild the row and refresh album rows referencing it.
	contentChanged := true
	if track.CoverImageID != nil {
		if old, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && old.Hash == hash {
			contentChanged = false
		}
	}

	// Stage files under temporary names; nothing on disk is mutated until
	// the new row exists (or, for a plain restore, until we are ready to
	// commit the files).
	tmpMain := mainPath + ".tmp"
	if err := os.WriteFile(tmpMain, data, 0644); err != nil {
		m.noteFail(track.ID)
		return err
	}
	cleanupTmp := func() {
		os.Remove(tmpMain)
		os.Remove(thumbPath + ".tmp")
	}

	var tmpThumb string
	if w > 64 || h > 64 {
		tmpThumb = thumbPath + ".tmp"
		if err := ResizeToThumbnail(data, tmpThumb, 64); err != nil {
			// A cover whose header decodes but whose pixel data is corrupt
			// (e.g. an embedded JPEG with intact SOF but broken entropy data)
			// fails here on the full decode. Treat the failure as fatal so the
			// broken image is never persisted as the track cover. Remove the
			// staged main file too, not just the thumbnail.
			cleanupTmp()
			m.noteFail(track.ID)
			return err
		}
	}
	thumbWritten := tmpThumb != "" && fileSize(tmpThumb) > 0

	commitFiles := func() error {
		if err := os.Rename(tmpMain, mainPath); err != nil {
			cleanupTmp()
			return err
		}
		if thumbWritten {
			if err := os.Rename(tmpThumb, thumbPath); err != nil {
				os.Remove(tmpThumb)
				cleanupTmp()
				return err
			}
		} else {
			// Small source: drop any stale thumbnail and reference the
			// original.
			os.Remove(thumbPath)
		}
		return nil
	}

	// rollbackNewRow undoes a committed pointer change and removes the new
	// row after a file-commit failure, so the track returns to the previous
	// row (its files may hold new bytes; the next scan re-syncs via hash).
	rollbackNewRow := func(newImgID, prevImageID string) {
		if prevImageID != "" {
			m.tracks.SetCoverImage(ctx, track.ID, prevImageID)
		}
		m.images.Delete(ctx, newImgID)
	}

	if contentChanged {
		variants := domain.ImageVariants{}
		if thumbWritten {
			sw, sh := scaledDims(w, h, 64)
			variants = append(variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(tmpThumb)})
		} else {
			variants = append(variants, domain.ImageVariant{Path: mainPath, Width: w, Height: h, Size: int64(len(data))})
		}

		img := &domain.Image{
			ID:        domain.NewID(),
			LibraryID: libraryID,
			OwnerType: "track",
			OwnerID:   track.ID,
			Source:    source,
			Path:      mainPath,
			Format:    format,
			Width:     w,
			Height:    h,
			Size:      int64(len(data)),
			Hash:      hash,
			Variants:  variants,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := m.images.Create(ctx, img); err != nil {
			cleanupTmp()
			m.noteFail(track.ID)
			return err
		}

		// Repoint the track before touching the files: a commit failure then
		// rolls back the pointer and drops the new row, leaving the previous
		// row authoritative.
		prevImageID := ""
		if track.CoverImageID != nil {
			prevImageID = *track.CoverImageID
		}
		if err := m.tracks.SetCoverImage(ctx, track.ID, img.ID); err != nil {
			m.images.Delete(ctx, img.ID)
			cleanupTmp()
			m.noteFail(track.ID)
			return err
		}

		if err := commitFiles(); err != nil {
			rollbackNewRow(img.ID, prevImageID)
			m.noteFail(track.ID)
			return err
		}

		// Files and pointer are committed; retire the stale row now.
		if prevImageID != "" {
			if err := m.images.Delete(ctx, prevImageID); err != nil {
				log.Printf("[cover] delete stale track image row: %v", err)
			}
		}
		track.CoverImageID = &img.ID

		m.refreshAlbumsReferencing(ctx, mainPath, data, w, h)
	} else {
		// Plain restore: content identical, only the files were missing.
		if err := commitFiles(); err != nil {
			m.noteFail(track.ID)
			return err
		}
	}
	m.clearFail(track.ID)

	if album != nil && album.CoverImageID == nil {
		if id, err := m.createAlbumImage(ctx, album, mainPath, data, w, h, source); err == nil {
			album.CoverImageID = &id
			if err := m.albums.Update(ctx, album); err != nil {
				log.Printf("[cover] album cover update error for %s: %v", album.ID, err)
				if derr := m.images.Delete(ctx, id); derr != nil {
					log.Printf("[cover] rollback album image row %s: %v", id, derr)
				}
				album.CoverImageID = nil
				m.noteFail("album:" + album.ID)
			} else {
				m.clearFail("album:" + album.ID)
			}
		} else {
			log.Printf("[cover] album cover create error for %s: %v", album.ID, err)
			m.noteFail("album:" + album.ID)
		}
	}
	return nil
}

// BackfillAlbumCover creates the album cover from its first cover-bearing
// track's original file without re-extracting the track. Non-destructive:
// the previous album row is only removed after the replacement row exists.
func (m *CoverManager) BackfillAlbumCover(ctx context.Context, album *domain.Album, force bool) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	key := "album:" + album.ID
	if !force && m.recentFail(key) {
		return fmt.Errorf("album cover backfill recently failed for %s", album.ID)
	}
	// Concurrent requests serialize on the lock; if a previous request
	// already restored every file (original and variants), nothing to do.
	if !force && album.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *album.CoverImageID); err == nil && imageFilesIntact(img) {
			return nil
		}
	}

	tracks, err := m.tracks.FindByAlbumID(ctx, album.ID)
	if err != nil {
		return err
	}
	var trackPath string
	var data []byte
	var w, h int
	var trackImgSource string
	for i := range tracks {
		if tracks[i].CoverImageID == nil {
			continue
		}
		img, err := m.images.FindByID(ctx, *tracks[i].CoverImageID)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(img.Path)
		if err != nil {
			continue
		}
		trackPath = img.Path
		trackImgSource = img.Source
		data = b
		_, w, h, _ = imageInfo(b)
		break
	}
	if trackPath == "" {
		m.noteFail(key)
		return fmt.Errorf("no usable cover-bearing track for album %s", album.ID)
	}

	// Content unchanged (a plain restore of deleted thumbnail files): keep
	// the existing row and cover_image_id, only rewrite the 256px file so
	// clients holding the current image id keep working.
	newHash := contentHash(data)
	if album.CoverImageID != nil {
		if stale, serr := m.images.FindByID(ctx, *album.CoverImageID); serr == nil && stale.Hash == newHash {
			if w > 256 || h > 256 {
				thumbDir := filepath.Join(m.imagesDir, "album")
				if err := os.MkdirAll(thumbDir, 0755); err != nil {
					log.Printf("[cover] album thumbnail dir create error for %s: %v", album.ID, err)
				} else {
					thumbPath := filepath.Join(thumbDir, fmt.Sprintf("album_%s_256.jpg", album.ID))
					if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
						log.Printf("[cover] album thumbnail resize error for %s: %v", album.ID, err)
					}
				}
			}
			m.clearFail(key)
			return nil
		}
	}

	// Content changed: build the replacement first (the new 256px file
	// overwrites the deterministic path — the old bytes are being replaced
	// anyway), then repoint the album, and only then retire the stale row.
	prevImageID := ""
	if album.CoverImageID != nil {
		prevImageID = *album.CoverImageID
	}
	newID, err := m.createAlbumImage(ctx, album, trackPath, data, w, h, trackImgSource)
	if err != nil {
		m.noteFail(key)
		return err
	}

	album.CoverImageID = &newID
	if err := m.albums.Update(ctx, album); err != nil {
		// The new row would be orphaned; drop it and keep the previous row
		// authoritative so repeated requests do not accumulate rows.
		if derr := m.images.Delete(ctx, newID); derr != nil {
			log.Printf("[cover] rollback album image row %s: %v", newID, derr)
		}
		m.noteFail(key)
		return err
	}

	// Album pointer persisted; retire the stale row (never the referenced
	// track original).
	if prevImageID != "" {
		if err := m.images.Delete(ctx, prevImageID); err != nil {
			log.Printf("[cover] delete stale album image row: %v", err)
		}
	}
	m.clearFail(key)
	return nil
}

// createAlbumImage builds an album cover row that references a track's
// original file. The 256px thumbnail is written to the album directory only
// when the source is larger and the resize succeeds; otherwise the variant
// references the original. Album covers are shared across libraries and
// carry no library id. source records where the cover came from
// ("embedded"/"network") so the row does not mislabel a network cover.
// Returns the new images-row id.
func (m *CoverManager) createAlbumImage(ctx context.Context, album *domain.Album, trackMainPath string, data []byte, w, h int, source string) (string, error) {
	variants := domain.ImageVariants{}
	thumbPath := filepath.Join(m.imagesDir, "album", fmt.Sprintf("album_%s_256.jpg", album.ID))
	if w > 256 || h > 256 {
		thumbDir := filepath.Join(m.imagesDir, "album")
		if err := os.MkdirAll(thumbDir, 0755); err != nil {
			return "", err
		}
		if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
			log.Printf("[cover] album thumbnail resize error for %s: %v", album.ID, err)
		}
		if fileSize(thumbPath) > 0 {
			sw, sh := scaledDims(w, h, 256)
			variants = append(variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(thumbPath)})
		}
	} else {
		// Small source: drop any stale thumbnail and reference the original.
		os.Remove(thumbPath)
	}
	if len(variants) == 0 {
		variants = append(variants, domain.ImageVariant{Path: trackMainPath, Width: w, Height: h, Size: int64(len(data))})
	}

	img := &domain.Image{
		ID:        domain.NewID(),
		OwnerType: "album",
		OwnerID:   album.ID,
		Source:    source,
		Path:      trackMainPath,
		Format:    imageFormat(data),
		Width:     w,
		Height:    h,
		Size:      int64(len(data)),
		Hash:      contentHash(data),
		Variants:  variants,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := m.images.Create(ctx, img); err != nil {
		return "", err
	}
	return img.ID, nil
}

// refreshAlbumsReferencing updates album rows whose original points at the
// given track path after the track was re-extracted: metadata (format,
// dimensions, hash, size) is refreshed and the 256px thumbnail regenerated
// so album covers do not serve stale data.
func (m *CoverManager) refreshAlbumsReferencing(ctx context.Context, path string, data []byte, w, h int) {
	imgs, err := m.images.FindByPath(ctx, "album", path)
	if err != nil {
		log.Printf("[cover] find albums referencing %s: %v", path, err)
		return
	}
	for i := range imgs {
		img := &imgs[i]
		img.Format = imageFormat(data)
		img.Width = w
		img.Height = h
		img.Size = int64(len(data))
		img.Hash = contentHash(data)
		img.UpdatedAt = time.Now()
		img.Variants = domain.ImageVariants{}
		thumbPath := filepath.Join(m.imagesDir, "album", fmt.Sprintf("album_%s_256.jpg", img.OwnerID))
		if w > 256 || h > 256 {
			if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
				log.Printf("[cover] refresh thumbnail resize error for %s: %v", img.OwnerID, err)
			}
			if fileSize(thumbPath) > 0 {
				sw, sh := scaledDims(w, h, 256)
				img.Variants = append(img.Variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(thumbPath)})
			}
		} else {
			// Small source: drop any stale thumbnail and reference the
			// original.
			os.Remove(thumbPath)
		}
		if len(img.Variants) == 0 {
			img.Variants = append(img.Variants, domain.ImageVariant{Path: path, Width: w, Height: h, Size: int64(len(data))})
		}
		if err := m.images.Update(ctx, img); err != nil {
			log.Printf("[cover] refresh album row %s: %v", img.ID, err)
		}
	}
}

// DeleteTrackCovers removes the track's images row(s) and files, plus any
// album rows that referenced the track original (their covers become null
// through the foreign key).
func (m *CoverManager) DeleteTrackCovers(ctx context.Context, libraryID, trackID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	for _, ext := range []string{"jpg", "png"} {
		mainPath := CoverPath(m.imagesDir, libraryID, "track", trackID, ext)
		if err := m.deleteAlbumsReferencing(ctx, mainPath); err != nil {
			return err
		}
	}
	if err := m.images.DeleteByOwner(ctx, "track", trackID); err != nil {
		return err
	}
	os.Remove(CoverPath(m.imagesDir, libraryID, "track", trackID, "jpg"))
	os.Remove(CoverPath(m.imagesDir, libraryID, "track", trackID, "png"))
	os.Remove(CoverPathWithSuffix(m.imagesDir, libraryID, "track", trackID, "_64", "jpg"))
	return nil
}

// DeleteAlbumCovers removes the album's images row(s) and files.
func (m *CoverManager) DeleteAlbumCovers(ctx context.Context, albumID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	if err := m.images.DeleteByOwner(ctx, "album", albumID); err != nil {
		return err
	}
	RemoveAlbumCover(m.imagesDir, albumID)
	return nil
}

// DeleteArtistCovers removes the artist's images row(s) and files.
func (m *CoverManager) DeleteArtistCovers(ctx context.Context, artistID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	if err := m.images.DeleteByOwner(ctx, "artist", artistID); err != nil {
		return err
	}
	os.Remove(CoverPath(m.imagesDir, "artist", "artist", artistID, "jpg"))
	return nil
}

// SweepOrphanCovers removes image rows (and their files) whose owning entity
// no longer exists. Normal flows clean covers before deleting the entity
// (and keep the row on failure), so orphans indicate an interrupted deletion
// or manual database edits. Best-effort: a row deletion failure is logged and
// the sweep continues, so one stuck row cannot block the rest. Physical files
// are only removed when no other (live) images row still references them —
// album covers share a track's original path and must not be deleted out
// from under the track.
func (m *CoverManager) SweepOrphanCovers(ctx context.Context) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	imgs, err := m.images.FindOrphans(ctx)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		// Remove the files while the row still exists: a failed file removal
		// keeps the row so the next sweep retries. (Deleting the row first
		// would leak the files permanently — FindOrphans can no longer see
		// them.) Only after every file was handled is the row dropped; a
		// failed row deletion leaves the row pointing at already-gone files,
		// which the next sweep self-heals via idempotent removes.
		if err := m.removeOrphanCoverFiles(ctx, img); err != nil {
			log.Printf("[cover] orphan cover files for %s kept for retry: %v", img.ID, err)
			continue
		}
		if err := m.images.Delete(ctx, img.ID); err != nil {
			log.Printf("[cover] delete orphan images row %s: %v", img.ID, err)
		}
	}
	return nil
}

// removeOrphanCoverFiles deletes an orphan's own files, skipping any path
// still referenced by another images row (album rows share a track's
// original). An error is returned when any path could not be counted or
// removed, so the caller keeps the row and retries next sweep. A path still
// referenced by a live row is not an error — the shared file is kept and the
// row is still safe to drop.
func (m *CoverManager) removeOrphanCoverFiles(ctx context.Context, img domain.Image) error {
	paths := []string{img.Path}
	for _, v := range img.Variants {
		paths = append(paths, v.Path)
	}
	retry := false
	for _, p := range paths {
		if p == "" {
			continue
		}
		n, err := m.images.CountPathExcept(ctx, p, img.ID)
		if err != nil {
			// A failed count leaves the file on disk; keep the row so the
			// leak stays retryable and observable.
			log.Printf("[cover] count refs for orphan cover %s: %v", p, err)
			retry = true
			continue
		}
		if n > 0 {
			continue // still referenced by a live row; keep the file
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("[cover] remove orphan cover file %s: %v", p, err)
			retry = true
		}
	}
	if retry {
		return errors.New("some orphan cover files were not removed")
	}
	return nil
}

func (m *CoverManager) deleteAlbumsReferencing(ctx context.Context, path string) error {
	imgs, err := m.images.FindByPath(ctx, "album", path)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		if err := m.images.Delete(ctx, img.ID); err != nil {
			return err
		}
		// The albums.cover_image_id column clears via ON DELETE SET NULL.
		RemoveAlbumCover(m.imagesDir, img.OwnerID)
	}
	return nil
}

// imageFilesIntact reports whether the original and every recorded variant
// of an images row exist on disk. Used by the locked re-check so a
// concurrent request that just restored the files short-circuits.
func imageFilesIntact(img *domain.Image) bool {
	if !CoverFileExists(img.Path) {
		return false
	}
	for _, v := range img.Variants {
		if !CoverFileExists(v.Path) {
			return false
		}
	}
	return true
}

// ImageVariantPath picks the file to serve for a requested size: the largest
// variant that fits within the target (64/256); when the best fitting variant
// is far below the target (e.g. a track's 64px thumb for a 256 request) or no
// variant fits, the original is served. A missing or zero size resolves to
// the original.
func ImageVariantPath(img *domain.Image, size int) string {
	target := 0
	switch {
	case size > 256:
		target = 0
	case size > 64:
		target = 256
	case size > 0:
		target = 64
	default:
		target = 0
	}
	if target == 0 {
		return img.Path
	}
	best := ""
	bestWidth := 0
	for _, v := range img.Variants {
		if v.Width > 0 && v.Width <= target && v.Width > bestWidth {
			best = v.Path
			bestWidth = v.Width
		}
	}
	if best != "" && bestWidth >= target/2 {
		return best
	}
	return img.Path
}

// ensureJPEG re-encodes non-JPEG cover bytes (PNG/GIF/WebP/...) as JPEG so
// every original is stored as .jpg. Payloads that cannot be decoded (e.g.
// AVIF) are rejected so they are never persisted under a .jpg path with a
// wrong content type. JPEG input is detected via the header only and returned
// as-is — a full decode is only performed for formats that actually need
// re-encoding, keeping the memory cost of a large cover bounded.
func ensureJPEG(data []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode cover image: %w", err)
	}
	// Reject only empty canvases (a validity check). No pixel-size ceiling is
	// enforced here: this function serves the local embedded-cover path, and
	// local files are trusted. The network path applies its own
	// maxCoverDimension cap in fetchImageWithClient before the download.
	if cfg.Width == 0 || cfg.Height == 0 {
		return nil, fmt.Errorf("decode cover image: empty dimensions")
	}
	if format == "jpeg" {
		return data, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode cover image: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode cover as jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func scaledDims(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	scale := float64(max) / float64(w)
	if h > w {
		scale = float64(max) / float64(h)
	}
	return int(float64(w) * scale), int(float64(h) * scale)
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func imageFormat(data []byte) string {
	format, _, _, _ := imageInfo(data)
	return format
}

func imageInfo(data []byte) (format string, w, h int, hash string) {
	if cfg, f, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		format, w, h = f, cfg.Width, cfg.Height
	}
	return format, w, h, contentHash(data)
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
