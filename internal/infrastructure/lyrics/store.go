package lyrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	PriorityEmbedded = 0
	PrioritySidecar  = 1
	PriorityNetwork  = 2
	PriorityUser     = 3
)

func PriorityBit(priority int) int {
	return 1 << uint(priority)
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Dir() string {
	return s.dir
}

// Save writes lyrics content and returns the bitmask bit for this priority.
func (s *Store) Save(libraryID, trackID string, priority int, content string) error {
	libDir := filepath.Join(s.dir, libraryID)
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lyrics dir: %w", err)
	}

	base := filepath.Join(libDir, fmt.Sprintf("%s_p%d", trackID, priority))
	for _, ext := range []string{".txt", ".lrc"} {
		os.Remove(base + ext)
	}

	format := detectFormat(content)
	path := base + "." + format
	return os.WriteFile(path, []byte(content), 0644)
}

// Get returns the highest-priority lyrics whose bit is set in mask.
// actualMask is the bitmask of priorities that actually have files on disk
// (may differ from mask if files were deleted). Callers should update the
// database if actualMask != mask.
func (s *Store) Get(libraryID, trackID string, mask int) (content string, priority int, format string, actualMask int, err error) {
	libDir := filepath.Join(s.dir, libraryID)
	checkOrder := []int{PriorityUser, PriorityNetwork, PrioritySidecar, PriorityEmbedded}

	for _, prio := range checkOrder {
		if mask&PriorityBit(prio) == 0 {
			continue
		}
		base := filepath.Join(libDir, fmt.Sprintf("%s_p%d", trackID, prio))
		for _, ext := range []string{".txt", ".lrc"} {
			data, readErr := os.ReadFile(base + ext)
			if readErr == nil {
				actualMask |= PriorityBit(prio)
				if content == "" {
					f := "txt"
					if ext == ".lrc" {
						f = "lrc"
					}
					content, priority, format = string(data), prio, f
				}
			}
		}
	}
	if content == "" {
		return "", 0, "", 0, fmt.Errorf("no lyrics found")
	}
	return content, priority, format, actualMask, nil
}

func (s *Store) Delete(libraryID, trackID string, priority int) error {
	libDir := filepath.Join(s.dir, libraryID)
	base := filepath.Join(libDir, fmt.Sprintf("%s_p%d", trackID, priority))
	var errs []string
	for _, ext := range []string{".txt", ".lrc"} {
		if err := os.Remove(base + ext); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delete lyrics: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Store) DeleteAll(libraryID, trackID string) error {
	libDir := filepath.Join(s.dir, libraryID)
	pattern := filepath.Join(libDir, fmt.Sprintf("%s_p*", trackID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, m := range matches {
		os.Remove(m)
	}
	return nil
}

func detectFormat(content string) string {
	if len(content) == 0 {
		return "txt"
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 1 && line[0] == '[' {
			closeBracket := strings.IndexByte(line, ']')
			if closeBracket > 1 && closeBracket < 10 {
				timePart := line[1:closeBracket]
				if strings.Contains(timePart, ":") {
					return "lrc"
				}
			}
		}
	}
	return "txt"
}
