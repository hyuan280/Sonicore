package transcoder

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var (
	runningMu sync.Mutex
	running   = map[string]bool{}
)

// fmp4Fragment describes one moof+mdat fragment: its start time in seconds
// (from tfdt) and its byte range within the file.
type fmp4Fragment struct {
	start  float64
	offset int64
	size   int64
}

// fmp4Index is a parsed view of a fragmented MP4: the init segment (ftyp+moov)
// byte length, the per-fragment layout, and the total covered duration.
type fmp4Index struct {
	initSize int64
	duration float64
	frags    []fmp4Fragment
}

func readBoxAt(r io.ReaderAt, off int64) (size int64, typ string, end int64, ok bool) {
	var hdr [16]byte
	if _, err := r.ReadAt(hdr[:8], off); err != nil {
		return 0, "", off, false
	}
	size = int64(binary.BigEndian.Uint32(hdr[:4]))
	typ = string(hdr[4:8])
	if size == 1 {
		if _, err := r.ReadAt(hdr[8:16], off+8); err != nil {
			return 0, "", off, false
		}
		size = int64(binary.BigEndian.Uint64(hdr[8:16]))
	} else if size == 0 {
		size = -1
	}
	if size == -1 {
		return size, typ, -1, true
	}
	return size, typ, off + size, true
}

// findBox scans boxes within [start,end] and returns the first of type want.
func findBox(r io.ReaderAt, start, end int64, want string) (off, size int64, ok bool) {
	pos := start
	for pos+8 <= end {
		size, typ, bend, bok := readBoxAt(r, pos)
		if !bok || size == -1 || size < 8 {
			break
		}
		if typ == want {
			return pos, size, true
		}
		pos = bend
	}
	return 0, 0, false
}

// readTimescaleValue reads the timescale field of an mdhd/mvhd full box.
func readTimescaleValue(r io.ReaderAt, boxOff int64) (uint32, bool) {
	var b [4]byte
	if _, err := r.ReadAt(b[:4], boxOff+8); err != nil {
		return 0, false
	}
	var tsOff int64
	if b[0] == 1 {
		tsOff = boxOff + 8 + 4 + 8 + 8
	} else {
		tsOff = boxOff + 8 + 4 + 4 + 4
	}
	var ts [4]byte
	if _, err := r.ReadAt(ts[:4], tsOff); err != nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(ts[:4]), true
}

// findTimescale reads the track timescale (moov>trak>mdia>mdhd) inside a moov.
func findTimescale(r io.ReaderAt, moovOff, moovSize int64) (uint32, bool) {
	end := moovOff + moovSize
	pos := moovOff + 8
	for pos+8 <= end {
		size, typ, bend, ok := readBoxAt(r, pos)
		if !ok || size == -1 || size < 8 {
			break
		}
		if typ == "trak" {
			if mdiaOff, mdiaSize, mok := findBox(r, pos+8, bend, "mdia"); mok {
				if mdhdOff, _, dok := findBox(r, mdiaOff+8, mdiaOff+mdiaSize, "mdhd"); dok {
					if ts, tOK := readTimescaleValue(r, mdhdOff); tOK {
						return ts, true
					}
				}
			}
		}
		pos = bend
	}
	return 0, false
}

// readTfdtValue reads the base media decode time of a tfdt full box.
func readTfdtValue(r io.ReaderAt, boxOff int64) (uint64, bool) {
	var b [4]byte
	if _, err := r.ReadAt(b[:4], boxOff+8); err != nil {
		return 0, false
	}
	if b[0] == 1 {
		var v [8]byte
		if _, err := r.ReadAt(v[:8], boxOff+12); err != nil {
			return 0, false
		}
		return binary.BigEndian.Uint64(v[:8]), true
	}
	var v [4]byte
	if _, err := r.ReadAt(v[:4], boxOff+12); err != nil {
		return 0, false
	}
	return uint64(binary.BigEndian.Uint32(v[:4])), true
}

// findTfdt reads the base media decode time (moof>traf>tfdt) inside a moof.
func findTfdt(r io.ReaderAt, moofOff, moofSize int64) (uint64, bool) {
	end := moofOff + moofSize
	pos := moofOff + 8
	for pos+8 <= end {
		size, typ, bend, ok := readBoxAt(r, pos)
		if !ok || size == -1 || size < 8 {
			break
		}
		if typ == "traf" {
			if tfdtOff, _, tok := findBox(r, pos+8, bend, "tfdt"); tok {
				return readTfdtValue(r, tfdtOff)
			}
		}
		pos = bend
	}
	return 0, false
}

// buildFmp4Index parses a (possibly still-growing) fragmented MP4 file and
// returns the init-segment length, per-fragment byte ranges and start times,
// and the total covered duration. Incomplete trailing boxes are ignored so the
// file can be parsed while it is still being written.
func buildFmp4Index(f *os.File) (fmp4Index, error) {
	info, err := f.Stat()
	if err != nil {
		return fmp4Index{}, err
	}
	fileSize := info.Size()
	idx := fmp4Index{}

	var timescale uint32
	off := int64(0)
	for off+8 <= fileSize {
		size, typ, end, ok := readBoxAt(f, off)
		if !ok || size == -1 || size < 8 {
			break
		}
		switch typ {
		case "moov":
			// only trust the init size once the whole moov is written
			if end <= fileSize {
				if ts, tOK := findTimescale(f, off, size); tOK {
					timescale = ts
				}
				idx.initSize = end
			}
		case "moof":
			// the rest of the file is partial once a box extends past EOF
			if end > fileSize {
				break
			}
			if timescale == 0 {
				timescale = 44100
			}
			if t, tOK := findTfdt(f, off, size); tOK {
				mdatEnd := end
				pos := end
				complete := true
				for pos+8 <= fileSize {
					ms, mtyp, mend, mok := readBoxAt(f, pos)
					if !mok || ms == -1 || ms < 8 {
						break
					}
					if mtyp == "mdat" {
						mdatEnd = mend
						if mend > fileSize {
							complete = false
						}
						break
					}
					pos = mend
				}
				if complete && mdatEnd > off {
					idx.frags = append(idx.frags, fmp4Fragment{
						start:  float64(t) / float64(timescale),
						offset: off,
						size:   mdatEnd - off,
					})
					off = mdatEnd
					continue
				}
			}
			// partial fragment or missing mdat: stop, the tail is incomplete
			break
		}
		off = end
	}

	if len(idx.frags) > 1 {
		step := idx.frags[1].start - idx.frags[0].start
		if step <= 0 {
			step = 1
		}
		idx.duration = idx.frags[len(idx.frags)-1].start + step
	} else if len(idx.frags) == 1 {
		idx.duration = idx.frags[0].start + 1
	}
	return idx, nil
}

// fragForTime returns the index of the fragment whose start time is <= t.
func fragForTime(idx fmp4Index, t float64) int {
	lo := 0
	for i, fr := range idx.frags {
		if fr.start <= t {
			lo = i
		} else {
			break
		}
	}
	return lo
}

// extractFragments returns the bytes from the fragment containing startSec up
// to and including the fragment containing endSec.
func extractFragments(f *os.File, idx fmp4Index, startSec, endSec float64) ([]byte, error) {
	a := fragForTime(idx, startSec)
	b := fragForTime(idx, endSec)
	from := idx.frags[a].offset
	to := idx.frags[b].offset + idx.frags[b].size
	buf := make([]byte, to-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// startTranscode launches a background transcode if the cache isn't ready and
// no transcode for this cache path is already running. It never blocks: segment
// requests proceed to serveMse* and poll the growing tmp file for their range.
func startTranscode(filePath, quality, dstPath string, cfg transcodeConfig) {
	runningMu.Lock()
	if running[dstPath] || cacheValid(dstPath, filePath) {
		runningMu.Unlock()
		return
	}
	running[dstPath] = true
	runningMu.Unlock()

	go func() {
		defer func() {
			runningMu.Lock()
			delete(running, dstPath)
			runningMu.Unlock()
		}()
		if recoverErr := recover(); recoverErr != nil {
			log.Printf("[transcoder] panic in background transcode: %v", recoverErr)
		}
		if cacheValid(dstPath, filePath) {
			return
		}
		if err := transcodeToFile(filePath, quality, dstPath, cfg); err != nil {
			log.Printf("[transcoder] background transcode error: %v", err)
		}
	}()
}

// currentTranscodeFile opens the completed cache file if present, otherwise the
// newest in-progress tmp file for the cache path.
func currentTranscodeFile(cPath, filePath string) *os.File {
	if cacheValid(cPath, filePath) {
		if f, err := os.Open(cPath); err == nil {
			return f
		}
		return nil
	}
	files, _ := filepath.Glob(cPath + ".tmp.*")
	var newest string
	var newestMod time.Time
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.Size() > 0 {
			if fi.ModTime().After(newestMod) {
				newest = f
				newestMod = fi.ModTime()
			}
		}
	}
	if newest == "" {
		return nil
	}
	if f, err := os.Open(newest); err == nil {
		return f
	}
	return nil
}

// ServeMse serves MediaSource-compatible data for a track: the init segment
// (ftyp+moov) when wantInit is set, otherwise the moof/mdat fragments covering
// [startSec, startSec+durSec]. A background transcode is started if needed and
// requests wait until the requested data has been transcoded.
func ServeMse(ctx context.Context, w http.ResponseWriter, filePath string, quality Quality, startSec, durSec float64, wantInit bool) {
	if cacheDir == "" {
		http.Error(w, "mse unavailable", http.StatusInternalServerError)
		return
	}
	cfg := resolveConfig(quality)
	cPath := cachePath(filePath, string(quality), cfg.ext)

	startTranscode(filePath, string(quality), cPath, cfg)

	if wantInit {
		serveMseInit(ctx, w, cPath, filePath)
		return
	}
	serveMseRange(ctx, w, cPath, filePath, startSec, durSec)
}

func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func serveMseInit(ctx context.Context, w http.ResponseWriter, cPath, filePath string) {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if f := currentTranscodeFile(cPath, filePath); f != nil {
			idx, err := buildFmp4Index(f)
			if fi, serr := f.Stat(); err == nil && serr == nil && idx.initSize > 8 && idx.initSize <= fi.Size() {
				buf := make([]byte, idx.initSize)
				if _, rerr := f.ReadAt(buf, 0); rerr == nil || rerr == io.EOF {
					f.Close()
					w.Header().Set("Content-Type", "audio/mp4")
					w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
					w.Header().Set("Cache-Control", "private, max-age=86400")
					w.Write(buf)
					return
				}
			}
			f.Close()
		}
		if ctxDone(ctx) {
			return
		}
		if time.Now().After(deadline) {
			http.Error(w, "transcode timeout", http.StatusInternalServerError)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func serveMseRange(ctx context.Context, w http.ResponseWriter, cPath, filePath string, startSec, durSec float64) {
	if durSec <= 0 {
		durSec = 5
	}
	need := startSec + durSec
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if f := currentTranscodeFile(cPath, filePath); f != nil {
			idx, err := buildFmp4Index(f)
			if err == nil && len(idx.frags) > 0 {
				if idx.duration >= need {
					if buf, xerr := extractFragments(f, idx, startSec, need); xerr == nil {
						f.Close()
						w.Header().Set("Content-Type", "audio/mp4")
						w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
						w.Header().Set("Cache-Control", "private, max-age=86400")
						w.Write(buf)
						return
					}
				} else if cacheValid(cPath, filePath) {
					if idx.duration > startSec {
						if buf, xerr := extractFragments(f, idx, startSec, idx.duration); xerr == nil {
							f.Close()
							w.Header().Set("Content-Type", "audio/mp4")
							w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
							w.Header().Set("Cache-Control", "private, max-age=86400")
							w.Write(buf)
							return
						}
					} else {
						f.Close()
						http.Error(w, "out of range", http.StatusRequestedRangeNotSatisfiable)
						return
					}
				}
			}
			f.Close()
		}
		if ctxDone(ctx) {
			return
		}
		if time.Now().After(deadline) {
			http.Error(w, "transcode timeout", http.StatusInternalServerError)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
