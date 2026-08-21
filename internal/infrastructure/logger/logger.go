package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Infof, Errorf, Warnf, Debugf format messages with fmt.Sprintf
// before passing them to slog. Use these when migrating from log.Printf.

type Config struct {
	Level      string
	Format     string
	FileOutput bool
	FilePath   string
	DataDir    string
	MaxSize    int
	MaxAge     int
	MaxBackups int
}

var (
	levelVar = new(slog.LevelVar)
	mu       sync.Mutex
	logFile  *lumberjack.Logger
	bufPool  = sync.Pool{
		New: func() any {
			b := make([]byte, 0, 256)
			return &b
		},
	}
)

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

type consoleHandler struct {
	w      io.Writer
	level  *slog.LevelVar
	attrs  []slog.Attr
	prefix string
}

func (h *consoleHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get().(*[]byte)
	*bp = (*bp)[:0]
	buf := *bp
	defer func() { *bp = buf }()
	defer bufPool.Put(bp)
	buf = r.Time.AppendFormat(buf, timeFormat)
	level := levelChar(r.Level)
	buf = append(buf, ' ')
	buf = append(buf, level)
	buf = append(buf, " "...)
	buf = append(buf, r.Message...)
	for _, a := range h.attrs {
		buf = appendAttr(buf, h.prefix, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		buf = appendAttr(buf, h.prefix, a)
		return true
	})
	buf = append(buf, '\n')
	_, err := h.w.Write(buf)
	return err
}

func appendAttr(buf []byte, prefix string, a slog.Attr) []byte {
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	buf = append(buf, ' ')
	buf = append(buf, key...)
	buf = append(buf, '=')
	if a.Value.Kind() == slog.KindGroup {
		buf = append(buf, '{')
		for i, ga := range a.Value.Group() {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = appendAttr(buf, key, ga)
		}
		buf = append(buf, '}')
	} else {
		buf = append(buf, a.Value.String()...)
	}
	return buf
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	combined := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(combined, h.attrs)
	copy(combined[len(h.attrs):], attrs)
	return &consoleHandler{w: h.w, level: h.level, attrs: combined, prefix: h.prefix}
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	prefix := name
	if h.prefix != "" {
		prefix = h.prefix + "." + name
	}
	return &consoleHandler{w: h.w, level: h.level, attrs: h.attrs, prefix: prefix}
}

func levelChar(l slog.Level) byte {
	switch {
	case l < slog.LevelInfo:
		return 'D'
	case l < slog.LevelWarn:
		return 'I'
	case l < slog.LevelError:
		return 'W'
	default:
		return 'E'
	}
}

func Init(cfg Config) error {
	mu.Lock()
	defer mu.Unlock()

	lvl, ok := ParseLevelOk(cfg.Level)
	if !ok {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, warning, error", cfg.Level)
	}
	levelVar.Set(lvl)

	if !cfg.FileOutput && logFile != nil {
		logFile.Close()
		logFile = nil
	}

	var writers []io.Writer
	writers = append(writers, os.Stderr)

	if cfg.FileOutput {
		path := cfg.FilePath
		if path == "" {
			path = filepath.Join(cfg.DataDir, "log", "sonicore.log")
		}
		lf := &lumberjack.Logger{
			Filename:   path,
			MaxSize:    cfg.MaxSize,
			MaxAge:     cfg.MaxAge,
			MaxBackups: cfg.MaxBackups,
			LocalTime:  true,
			Compress:   true,
		}
		writers = append(writers, lf)
		old := logFile
		logFile = lf
		if old != nil {
			old.Close()
		}
	}

	var writer io.Writer
	if len(writers) == 1 {
		writer = writers[0]
	} else {
		writer = io.MultiWriter(writers...)
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: levelVar})
	default:
		handler = &consoleHandler{w: writer, level: levelVar}
	}

	slog.SetDefault(slog.New(handler))
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())
	return nil
}

var _ slog.Handler = (*consoleHandler)(nil)

func SetLevel(level string) {
	levelVar.Set(parseLevel(level))
}

func ParseLevelOk(level string) (slog.Level, bool) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

func parseLevel(level string) slog.Level {
	l, _ := ParseLevelOk(level)
	return l
}

func Debug(msg string, args ...any) {
	if len(args) > 0 {
		slog.Debug(fmt.Sprintf(msg, args...))
	} else {
		slog.Debug(msg)
	}
}

func Info(msg string, args ...any) {
	if len(args) > 0 {
		slog.Info(fmt.Sprintf(msg, args...))
	} else {
		slog.Info(msg)
	}
}

func Warn(msg string, args ...any) {
	if len(args) > 0 {
		slog.Warn(fmt.Sprintf(msg, args...))
	} else {
		slog.Warn(msg)
	}
}

func Error(msg string, args ...any) {
	if len(args) > 0 {
		slog.Error(fmt.Sprintf(msg, args...))
	} else {
		slog.Error(msg)
	}
}
