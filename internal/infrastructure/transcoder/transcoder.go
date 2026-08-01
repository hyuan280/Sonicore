package transcoder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Quality string

const (
	QualityLossless Quality = "lossless"
	QualityHigh     Quality = "high"
	QualityStandard Quality = "standard"
)

const targetBitrateSQ = 256000
const targetBitrateHQ = 320000

var browserNativeCodecs = map[string]bool{
	"pcm_s16le":  true,
	"pcm_s24le":  true,
	"pcm_f32le":  true,
	"pcm_s16be":  true,
	"pcm_u8":     true,
	"pcm_s32le":  true,
	"pcm_u16le":  true,
	"pcm_f64le":  true,
	"mp3":        true,
	"libmp3lame": true,
	"aac":        true,
	"vorbis":     true,
	"opus":       true,
	"flac":       true,
}

var (
	cacheDir string
	initOnce sync.Once

	inflightMu sync.Mutex
	inflight   = map[string]chan struct{}{}
)

// lockInflight dedupes concurrent transcodes of the same cache path. If another
// request is already transcoding the path, it blocks until that finishes and
// returns a no-op release (the caller must re-check the cache afterwards).
func lockInflight(key string) func() {
	inflightMu.Lock()
	if ch, ok := inflight[key]; ok {
		inflightMu.Unlock()
		<-ch
		return func() {}
	}
	ch := make(chan struct{})
	inflight[key] = ch
	inflightMu.Unlock()
	return func() {
		inflightMu.Lock()
		delete(inflight, key)
		close(ch)
		inflightMu.Unlock()
	}
}

func Init(dir string) error {
	var err error
	initOnce.Do(func() {
		cacheDir = filepath.Join(dir, "transcode")
		if mkErr := os.MkdirAll(cacheDir, 0755); mkErr != nil {
			err = fmt.Errorf("transcoder cache dir: %w", mkErr)
			return
		}
		go cacheCleaner()
	})
	return err
}

func cacheCleaner() {
	for {
		time.Sleep(24 * time.Hour)
		if cacheDir == "" {
			continue
		}
		now := time.Now()
		entries, _ := os.ReadDir(cacheDir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > 7*24*time.Hour {
				os.Remove(filepath.Join(cacheDir, e.Name()))
			}
		}
	}
}

func cacheKey(filePath, quality string) string {
	h := md5.Sum([]byte(filePath))
	return fmt.Sprintf("%x_%s", h, quality)
}

func cachePath(filePath, quality, ext string) string {
	return filepath.Join(cacheDir, cacheKey(filePath, quality)+ext)
}

func cacheValid(cachePath, sourcePath string) bool {
	ci, err := os.Stat(cachePath)
	if err != nil {
		return false
	}
	si, err := os.Stat(sourcePath)
	if err != nil {
		return false
	}
	return ci.ModTime().After(si.ModTime())
}

// CacheReady reports whether a complete transcode of filePath at the given
// quality is available on disk (i.e. seeking can be served directly).
func CacheReady(filePath string, quality Quality) bool {
	if cacheDir == "" {
		return false
	}
	cfg := resolveConfig(quality)
	return cacheValid(cachePath(filePath, string(quality), cfg.ext), filePath)
}

func codecPlayable(audioCodec string) bool {
	if audioCodec == "" {
		return true
	}
	return browserNativeCodecs[audioCodec]
}

type Decision struct {
	Transcode bool
}

func Decide(trackBitrate int, audioCodec string, quality Quality) Decision {
	playable := codecPlayable(audioCodec)

	if quality == QualityLossless {
		return Decision{Transcode: !playable}
	}

	target := targetBitrateHQ
	if quality == QualityStandard {
		target = targetBitrateSQ
	}

	if !playable {
		return Decision{Transcode: true}
	}

	if trackBitrate > 0 && trackBitrate > target {
		return Decision{Transcode: true}
	}

	return Decision{Transcode: false}
}

type transcodeConfig struct {
	format       string
	codec        string
	bitrate      string
	contentType  string
	ext          string
	movFlags     string
	fragDuration int
	experimental bool
	channels     int
}

func resolveConfig(q Quality) transcodeConfig {
	switch q {
	case QualityLossless:
		return transcodeConfig{
			format:       "mp4",
			codec:        "flac",
			contentType:  "audio/mp4",
			ext:          ".m4a",
			movFlags:     "+frag_keyframe+empty_moov+default_base_moof",
			fragDuration: 1000000,
			experimental: true,
		}
	case QualityHigh:
		return transcodeConfig{
			format:       "mp4",
			codec:        "aac",
			bitrate:      "320k",
			contentType:  "audio/mp4",
			ext:          ".m4a",
			movFlags:     "+frag_keyframe+empty_moov+default_base_moof",
			fragDuration: 1000000,
			channels:     2,
		}
	default:
		return transcodeConfig{
			format:       "mp4",
			codec:        "aac",
			bitrate:      "256k",
			contentType:  "audio/mp4",
			ext:          ".m4a",
			movFlags:     "+frag_keyframe+empty_moov+default_base_moof",
			fragDuration: 1000000,
			channels:     2,
		}
	}
}

func ParseQuality(q string) Quality {
	switch q {
	case "lossless":
		return QualityLossless
	case "high":
		return QualityHigh
	default:
		return QualityStandard
	}
}

func ServeTranscoded(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath string, quality Quality) {
	cfg := resolveConfig(quality)

	if q := r.URL.Query(); q.Get("init") == "1" || q.Get("start") != "" {
		start, _ := strconv.ParseFloat(q.Get("start"), 64)
		dur, _ := strconv.ParseFloat(q.Get("duration"), 64)
		if dur <= 0 {
			dur = 5
		}
		ServeMse(ctx, w, filePath, quality, start, dur, q.Get("init") == "1")
		return
	}

	if cacheDir == "" {
		transcodeStream(ctx, w, filePath, string(quality), cfg)
		return
	}

	cPath := cachePath(filePath, string(quality), cfg.ext)
	if cacheValid(cPath, filePath) {
		log.Printf("[transcoder] cache hit: %s", cPath)
		serveCacheFile(w, r, cPath, cfg)
		return
	}

	// A "full" request (frontend seek during streaming) or a non-zero byte
	// Range request wants a complete, seekable file: wait for the transcode
	// (or run it) and then serve the finished cache.
	wantFull := r.URL.Query().Get("full") == "1" || isSeekRange(r.Header.Get("Range"))

	release := lockInflight(cPath)
	defer release()

	if cacheValid(cPath, filePath) {
		log.Printf("[transcoder] cache hit (concurrent): %s", cPath)
		serveCacheFile(w, r, cPath, cfg)
		return
	}

	if wantFull {
		log.Printf("[transcoder] waiting for transcode: %s", cPath)
		if err := transcodeToFile(filePath, string(quality), cPath, cfg); err != nil {
			log.Printf("[transcoder] transcode error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		serveCacheFile(w, r, cPath, cfg)
		return
	}

	log.Printf("[transcoder] transcoding (stream) %s → %s (%s)", filePath, cPath, quality)
	transcodeAndStream(w, filePath, string(quality), cPath, cfg)
}

// isSeekRange reports whether the Range header requests a non-zero start
// offset (i.e. the client is seeking, not doing an initial "bytes=0-" load).
func isSeekRange(rng string) bool {
	if !strings.HasPrefix(rng, "bytes=") {
		return false
	}
	start := strings.TrimPrefix(rng, "bytes=")
	if i := strings.IndexByte(start, '-'); i >= 0 {
		start = start[:i]
	}
	start = strings.TrimSpace(start)
	if start == "" {
		return false
	}
	n, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return false
	}
	return n > 0
}

func serveCacheFile(w http.ResponseWriter, r *http.Request, cPath string, cfg transcodeConfig) {
	w.Header().Set("Content-Type", cfg.contentType)
	http.ServeFile(w, r, cPath)
}

// transcodeToFile transcodes the source into the cache file. It is decoupled
// from the request context so a client disconnect does not abort the cache
// write; the next request then gets an instant cache hit.
func transcodeToFile(filePath, quality, dstPath string, cfg transcodeConfig) error {
	tmpPath := dstPath + ".tmp." + fmt.Sprintf("%x", time.Now().UnixNano())
	defer os.Remove(tmpPath)

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer tmpFile.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := buildFfmpegCmd(ctx, filePath, tmpPath, cfg)
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("[transcoder] ffmpeg start → %s (%s)", dstPath, quality)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			log.Printf("[transcoder] ffmpeg stderr:\n%s", msg)
		}
		return fmt.Errorf("ffmpeg failed: %s", takeLast(msg, 500))
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("cache rename: %w", err)
	}
	log.Printf("[transcoder] cache written: %s", dstPath)
	return nil
}

// transcodeAndStream starts playback immediately by streaming the fragmented
// MP4 to the client while writing the same bytes to the cache file. The
// transcode is decoupled from the request context so a client disconnect
// (e.g. the frontend reloading for a seek) does not abort the cache write.
func transcodeAndStream(w http.ResponseWriter, filePath, quality, dstPath string, cfg transcodeConfig) {
	tmpPath := dstPath + ".tmp." + fmt.Sprintf("%x", time.Now().UnixNano())
	defer os.Remove(tmpPath)

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}
	defer tmpFile.Close()

	tctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := buildFfmpegCmd(tctx, filePath, "pipe:1", cfg)
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}

	log.Printf("[transcoder] ffmpeg start → %s (%s)", dstPath, quality)

	stderrDone := readStderr(stderr)
	bufReader := bufio.NewReader(stdout)

	if err := waitForOutput(bufReader); err != nil {
		http.Error(w, fmt.Sprintf("ffmpeg failed: %s", takeLast(<-stderrDone, 500)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", cfg.contentType)
	w.WriteHeader(http.StatusOK)

	clientAlive := true
	buf := make([]byte, 64*1024)
	for {
		n, rerr := bufReader.Read(buf)
		if n > 0 {
			if _, werr := tmpFile.Write(buf[:n]); werr != nil {
				log.Printf("[transcoder] cache write error: %v", werr)
				cancel()
				break
			}
			if clientAlive {
				if _, werr := w.Write(buf[:n]); werr != nil {
					clientAlive = false
				}
			}
		}
		if rerr != nil {
			break
		}
	}

	runErr := cmd.Wait()
	stderrText := <-stderrDone
	cancel()

	if runErr != nil {
		log.Printf("[transcoder] ffmpeg failed: %s", takeLast(stderrText, 500))
		return
	}
	if err := tmpFile.Close(); err != nil {
		log.Printf("[transcoder] cache close error: %v", err)
		return
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		log.Printf("[transcoder] cache rename error: %v", err)
		return
	}
	log.Printf("[transcoder] cache written: %s", dstPath)
}

func transcodeStream(ctx context.Context, w http.ResponseWriter, filePath, quality string, cfg transcodeConfig) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := buildFfmpegCmd(ctx, filePath, "pipe:1", cfg)
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		log.Printf("[transcoder] pipe error: %v", err)
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[transcoder] ffmpeg start error: %v", err)
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}

	stderrDone := readStderr(stderr)
	bufReader := bufio.NewReader(stdout)

	if err := waitForOutput(bufReader); err != nil {
		stderrText := <-stderrDone
		if stderrText != "" {
			log.Printf("[transcoder] ffmpeg error: %s", stderrText)
		}
		http.Error(w, fmt.Sprintf("ffmpeg failed: %s", takeLast(stderrText, 200)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", cfg.contentType)
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	_, copyErr := io.Copy(w, bufReader)
	stderrText := <-stderrDone
	cancel()
	if err := cmd.Wait(); err != nil {
		log.Printf("[transcoder] ffmpeg failed: %s", takeLast(stderrText, 500))
	}

	if copyErr != nil {
		log.Printf("[transcoder] write error: %v", copyErr)
	}
}

func buildFfmpegCmd(ctx context.Context, filePath, output string, cfg transcodeConfig) *exec.Cmd {
	args := []string{
		"-y",
		"-i", filePath,
		"-f", cfg.format,
		"-vn",
		"-sn",
		"-dn",
		"-acodec", cfg.codec,
		"-map_metadata", "-1",
	}
	if cfg.bitrate != "" {
		args = append(args, "-b:a", cfg.bitrate)
	}
	if cfg.channels > 0 {
		args = append(args, "-ac", strconv.Itoa(cfg.channels))
	}
	if cfg.movFlags != "" {
		args = append(args, "-movflags", cfg.movFlags)
	}
	if cfg.fragDuration > 0 {
		args = append(args, "-frag_duration", strconv.Itoa(cfg.fragDuration))
	}
	if cfg.codec == "flac" {
		args = append(args, "-sample_fmt", "s16")
	}
	if cfg.experimental {
		args = append(args, "-strict", "-2")
	}
	args = append(args, output)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func setupCmdPipes(cmd *exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func readStderr(r io.ReadCloser) chan string {
	ch := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		ch <- strings.TrimSpace(string(data))
		close(ch)
	}()
	return ch
}

func waitForOutput(r *bufio.Reader) error {
	ch := make(chan error, 1)
	go func() {
		_, err := r.Peek(1)
		ch <- err
		close(ch)
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for ffmpeg output")
	}
}

func takeLast(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
