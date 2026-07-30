package transcoder

import (
	"bufio"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
)

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
	format      string
	codec       string
	bitrate     string
	contentType string
	ext         string
}

func resolveConfig(q Quality) transcodeConfig {
	switch q {
	case QualityLossless:
		return transcodeConfig{
			format:      "flac",
			codec:       "flac",
			contentType: "audio/flac",
			ext:         ".flac",
		}
	case QualityHigh:
		return transcodeConfig{
			format:      "adts",
			codec:       "aac",
			bitrate:     "320k",
			contentType: "audio/aac",
			ext:         ".aac",
		}
	default:
		return transcodeConfig{
			format:      "adts",
			codec:       "aac",
			bitrate:     "256k",
			contentType: "audio/aac",
			ext:         ".aac",
		}
	}
}

func ParseQuality(q string) Quality {
	switch q {
	case "lossless":
		return QualityLossless
	case "standard":
		return QualityStandard
	default:
		return QualityStandard
	}
}

func ServeTranscoded(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath string, quality Quality) {
	cfg := resolveConfig(quality)
	log.Printf("[transcoder] transcoding %s → %s", filePath, quality)

	if cacheDir != "" {
		cPath := cachePath(filePath, string(quality), cfg.ext)
		if cacheValid(cPath, filePath) {
			log.Printf("[transcoder] cache hit: %s", cPath)
			w.Header().Set("Content-Type", cfg.contentType)
			http.ServeFile(w, r, cPath)
			return
		}
		transcodeAndCache(ctx, w, r, filePath, string(quality), cPath, cfg)
		return
	}

	transcodeStream(ctx, w, filePath, string(quality), cfg)
}

func transcodeAndCache(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath, quality, dstPath string, cfg transcodeConfig) {
	tmpPath := dstPath + ".tmp." + fmt.Sprintf("%x", time.Now().UnixNano())
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		log.Printf("[transcoder] temp file error: %v", err)
		transcodeStream(ctx, w, filePath, quality, cfg)
		return
	}
	defer os.Remove(tmpPath)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := buildFfmpegCmd(ctx, filePath, cfg)
	stdout, stderr, err := setupCmdPipes(cmd)
	if err != nil {
		tmpFile.Close()
		log.Printf("[transcoder] pipe error: %v", err)
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		tmpFile.Close()
		log.Printf("[transcoder] ffmpeg start error: %v", err)
		http.Error(w, "transcoding error", http.StatusInternalServerError)
		return
	}

	log.Printf("[transcoder] ffmpeg start → %s (%s)", dstPath, quality)

	stderrDone := readStderr(stderr)
	bufReader := bufio.NewReader(stdout)

	if err := waitForOutput(bufReader); err != nil {
		stderrText := <-stderrDone
		if stderrText != "" {
			log.Printf("[transcoder] ffmpeg error: %s", stderrText)
		}
		tmpFile.Close()
		http.Error(w, fmt.Sprintf("ffmpeg failed: %s", takeLast(stderrText, 200)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", cfg.contentType)
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	multiWriter := io.MultiWriter(w, tmpFile)
	_, copyErr := io.Copy(multiWriter, bufReader)

	<-stderrDone
	tmpFile.Close()
	cancel()
	cmd.Wait()

	if copyErr != nil {
		log.Printf("[transcoder] write error: %v", copyErr)
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

	cmd := buildFfmpegCmd(ctx, filePath, cfg)
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
	<-stderrDone
	cancel()
	cmd.Wait()

	if copyErr != nil {
		log.Printf("[transcoder] write error: %v", copyErr)
	}
}

func buildFfmpegCmd(ctx context.Context, filePath string, cfg transcodeConfig) *exec.Cmd {
	args := []string{
		"-i", filePath,
		"-f", cfg.format,
		"-acodec", cfg.codec,
		"-map_metadata", "-1",
	}
	if cfg.bitrate != "" {
		args = append(args, "-b:a", cfg.bitrate)
	}
	if cfg.codec == "flac" {
		args = append(args, "-sample_fmt", "s16")
	}
	args = append(args, "pipe:1")

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
