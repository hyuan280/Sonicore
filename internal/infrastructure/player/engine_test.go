package player

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestEngine builds an engine that never talks to external audio tooling:
// no driver (no pactl) and no ffplay on PATH, so Play degrades gracefully.
func newTestEngine(t *testing.T, resolver TrackResolver) *Engine {
	t.Helper()
	t.Setenv("PATH", "")
	t.Setenv("PULSE_SERVER", "")
	return NewEngine("jb-1", "default", "", resolver)
}

func errResolver(err error) TrackResolver {
	return func(id string) (*TrackInfo, error) {
		return &TrackInfo{ID: id, Title: "T " + id, FilePath: "/m/" + id, Duration: 100}, err
	}
}

func TestNewEngineInitialState(t *testing.T) {
	e := NewEngine("jb-1", "dev-1", "alsa", nil)

	assert.Equal(t, "jb-1", e.ID())
	assert.Equal(t, "dev-1", e.DeviceID())

	st := e.Status()
	assert.Equal(t, StateStopped, st.State)
	assert.Equal(t, 0.8, st.Volume)
	assert.Equal(t, ModeNormal, st.PlayMode)
	assert.Empty(t, st.Queue)
	assert.Equal(t, 0, st.QueueIdx)
	assert.Nil(t, st.Track)
}

func TestRestoreState(t *testing.T) {
	e := NewEngine("jb-1", "d", "", nil)

	var emitted []Status
	e.OnChange(func(s Status) { emitted = append(emitted, s) })

	e.RestoreState([]string{"t-1", "t-2", "t-3"}, 1, []int{2, 0, 1}, 2, "shuffle", 0.5)

	st := e.Status()
	assert.Equal(t, []string{"t-1", "t-2", "t-3"}, st.Queue)
	assert.Equal(t, 1, st.QueueIdx)
	assert.Equal(t, []int{2, 0, 1}, st.ShuffleOrder)
	assert.Equal(t, 2, st.ShuffleIdx)
	assert.Equal(t, ModeShuffle, st.PlayMode)
	assert.Equal(t, 0.5, st.Volume)
	assert.Equal(t, StateStopped, st.State)
	assert.Nil(t, st.Track)
	require.Len(t, emitted, 1, "RestoreState emits")
}

func TestRestoreStateNilQueue(t *testing.T) {
	e := NewEngine("jb-1", "d", "", nil)
	e.RestoreState(nil, 0, nil, -1, "normal", 0.8)
	assert.Empty(t, e.Status().Queue, "nil queue normalized to empty")
}

func TestApplyPathMapping(t *testing.T) {
	e := NewEngine("jb-1", "d", "", nil)
	e.SetPathMapping(map[string]string{"lib-1": "/remote/music"}, map[string]string{"lib-1": "/local/music"})

	assert.Equal(t, "/remote/music/song.mp3", e.applyPathMapping("/local/music/song.mp3", "lib-1"))
	assert.Equal(t, "/elsewhere/song.mp3", e.applyPathMapping("/elsewhere/song.mp3", "lib-1"), "non-matching path untouched")

	// missing mapping for library → untouched
	assert.Equal(t, "/local/music/x.mp3", e.applyPathMapping("/local/music/x.mp3", "lib-other"))
}

func TestSetVolumeEmits(t *testing.T) {
	e := newTestEngine(t, nil)

	var last Status
	e.OnChange(func(s Status) { last = s })

	e.SetVolume(0.25)
	assert.Equal(t, 0.25, last.Volume)
	assert.Equal(t, 0.25, e.Status().Volume)
}

func TestSetPlayModeShuffle(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2", "t-3", "t-4"})
	e.queueIdx = 2 // "playing" the 3rd track

	e.SetPlayMode(ModeShuffle)

	st := e.Status()
	assert.Equal(t, ModeShuffle, st.PlayMode)
	require.Len(t, st.ShuffleOrder, 4)
	// permutation of 0..3
	sorted := append([]int(nil), st.ShuffleOrder...)
	sort.Ints(sorted)
	assert.Equal(t, []int{0, 1, 2, 3}, sorted)
	// current track position preserved in shuffle order
	assert.Equal(t, 2, st.ShuffleOrder[st.ShuffleIdx])
}

func TestSetPlayModeBackToNormal(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2"})
	e.SetPlayMode(ModeShuffle)
	e.SetPlayMode(ModeNormal)

	st := e.Status()
	assert.Equal(t, ModeNormal, st.PlayMode)
	// NOTE: leaving shuffle keeps the stale shuffle order (implementation quirk)
	assert.NotNil(t, st.ShuffleOrder)
}

func TestAddToQueueWhenStopped(t *testing.T) {
	e := newTestEngine(t, nil)
	e.AddToQueue([]string{"t-1", "t-2"})

	st := e.Status()
	assert.Equal(t, []string{"t-1", "t-2"}, st.Queue)
	assert.Equal(t, 0, st.QueueIdx, "starts at first added track")

	e.AddToQueue([]string{"t-3"})
	assert.Equal(t, []string{"t-1", "t-2", "t-3"}, e.Status().Queue)
}

func TestRemoveFromQueue(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.queueIdx = 1

	e.RemoveFromQueue(0) // remove before current
	assert.Equal(t, []string{"t-2", "t-3"}, e.Status().Queue)
	assert.Equal(t, 0, e.Status().QueueIdx, "index shifts down")

	e.RemoveFromQueue(1) // remove after current
	assert.Equal(t, []string{"t-2"}, e.Status().Queue)
	assert.Equal(t, 0, e.Status().QueueIdx)

	e.RemoveFromQueue(0)
	assert.Empty(t, e.Status().Queue)

	e.RemoveFromQueue(5) // out of range — no panic
	assert.Empty(t, e.Status().Queue)
}

func TestClearQueue(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2"})
	e.SetPlayMode(ModeShuffle)

	e.ClearQueue()

	st := e.Status()
	assert.Empty(t, st.Queue)
	assert.Equal(t, StateStopped, st.State)
	assert.Nil(t, st.ShuffleOrder)
	assert.Equal(t, -1, st.ShuffleIdx)
}

func TestShuffle(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2", "t-3"})

	e.Shuffle()

	st := e.Status()
	assert.Equal(t, ModeShuffle, st.PlayMode)
	require.Len(t, st.ShuffleOrder, 3)
	assert.Equal(t, -1, st.ShuffleIdx, "shuffle starts before the first track")
}

func TestSetQueueResetsIndex(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.queueIdx = 2

	e.SetQueue([]string{"t-9", "t-8"})

	st := e.Status()
	assert.Equal(t, []string{"t-9", "t-8"}, st.Queue)
	assert.Equal(t, 0, st.QueueIdx)
	assert.Equal(t, -1, st.ShuffleIdx)
}

func TestNextAdvancesQueue(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))

	var resolved string
	e.resolve = func(id string) (*TrackInfo, error) {
		resolved = id
		return &TrackInfo{ID: id, Title: "T", FilePath: "/m", Duration: 10}, errors.New("force degrade")
	}
	e.SetQueue([]string{"t-1", "t-2", "t-3"})

	e.Next()
	assert.Equal(t, "t-2", resolved, "advances to next track")
	assert.Equal(t, 1, e.Status().QueueIdx)
	assert.Equal(t, StateStopped, e.Status().State, "ffplay missing → graceful stop")
}

func TestNextAtQueueEndStops(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2"})
	e.queueIdx = 1

	e.Next()

	st := e.Status()
	assert.Equal(t, 1, st.QueueIdx, "does not advance past the end")
	assert.Equal(t, StateStopped, st.State)
}

func TestNextShuffle(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.SetPlayMode(ModeShuffle)

	// fix the shuffle order deterministically (white-box)
	e.mu.Lock()
	e.shuffleOrder = []int{2, 0, 1}
	e.shuffleIdx = 1 // currently on queue[0] = "t-1"
	e.mu.Unlock()

	var resolved string
	e.resolve = func(id string) (*TrackInfo, error) {
		resolved = id
		return &TrackInfo{ID: id}, errors.New("degrade")
	}

	e.Next()
	assert.Equal(t, "t-2", resolved, "follows shuffle order")
	assert.Equal(t, 2, e.Status().ShuffleIdx)
}

func TestPrev(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.queueIdx = 2

	var resolved string
	e.resolve = func(id string) (*TrackInfo, error) {
		resolved = id
		return &TrackInfo{ID: id}, errors.New("degrade")
	}

	e.Prev()
	assert.Equal(t, "t-2", resolved)
	assert.Equal(t, 1, e.Status().QueueIdx)
}

func TestPrevAtQueueStartStops(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2"})
	e.queueIdx = 0

	e.Prev()

	assert.Equal(t, 0, e.Status().QueueIdx)
	assert.Equal(t, StateStopped, e.Status().State)
}

func TestPlayNextRepeatOne(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2"})
	e.playMode = ModeRepeatOne
	e.current = &TrackInfo{ID: "t-1", Title: "T", FilePath: "/m", Duration: 10}

	e.playNext()

	assert.Equal(t, "t-1", e.Status().Track.ID, "repeat_one replays the same track")
}

func TestPlayNextRepeatAllWraps(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.queueIdx = 2
	e.playMode = ModeRepeatAll

	var resolved string
	e.resolve = func(id string) (*TrackInfo, error) {
		resolved = id
		return &TrackInfo{ID: id}, errors.New("degrade")
	}

	e.playNext()
	assert.Equal(t, "t-1", resolved, "wraps around to the first track")
	assert.Equal(t, 0, e.Status().QueueIdx)
}

func TestPlayNextNormalEndOfQueue(t *testing.T) {
	e := newTestEngine(t, nil)
	e.SetQueue([]string{"t-1", "t-2"})
	e.queueIdx = 1

	e.playNext()

	st := e.Status()
	assert.Equal(t, StateStopped, st.State)
	assert.Nil(t, st.Track)
}

func TestPlayNextShuffle(t *testing.T) {
	e := newTestEngine(t, errResolver(nil))
	e.SetQueue([]string{"t-1", "t-2", "t-3"})
	e.SetPlayMode(ModeShuffle)

	// fix the shuffle order deterministically (white-box)
	e.mu.Lock()
	e.shuffleOrder = []int{2, 0, 1}
	e.shuffleIdx = 1
	e.mu.Unlock()

	var resolved string
	e.resolve = func(id string) (*TrackInfo, error) {
		resolved = id
		return &TrackInfo{ID: id}, errors.New("degrade")
	}

	e.playNext()
	assert.Equal(t, "t-2", resolved, "follows shuffle order")
	assert.Equal(t, 2, e.Status().ShuffleIdx)
}

func TestStatusPositionClamped(t *testing.T) {
	e := NewEngine("jb-1", "d", "", nil)

	st := e.Status()
	assert.Equal(t, StateStopped, st.State)
	assert.Zero(t, st.Position, "no position while stopped")
	assert.Zero(t, st.Duration)

	e.current = &TrackInfo{ID: "t-1", Title: "T", FilePath: "/m", Duration: 100}
	e.state = StatePlaying
	e.trackStart = time.Now().Add(-2 * time.Second)

	st = e.Status()
	assert.Equal(t, 100.0, st.Duration)
	assert.LessOrEqual(t, st.Position, 100.0, "position clamped to duration")
	assert.Greater(t, st.Position, 0.0)
}

func TestOnChangeFires(t *testing.T) {
	e := newTestEngine(t, nil)

	called := 0
	var last State
	e.OnChange(func(s Status) {
		called++
		last = s.State
	})

	e.SetQueue([]string{"t-1"})
	e.SetVolume(0.5)
	e.SetPlayMode(ModeShuffle)

	assert.GreaterOrEqual(t, called, 3, "each mutation emits")
	assert.Equal(t, StateStopped, last)
}

// ---- EngineManager ----

func TestEngineManagerGetOrCreate(t *testing.T) {
	m := NewEngineManager()

	eng, created := m.GetOrCreate("jb-1", "dev-1", "alsa", nil)
	require.True(t, created)
	require.NotNil(t, eng)
	assert.Equal(t, "dev-1", eng.DeviceID())

	eng2, created2 := m.GetOrCreate("jb-1", "other-dev", "pulseaudio", nil)
	assert.False(t, created2, "second call reuses the engine")
	assert.Equal(t, eng, eng2)
	assert.Equal(t, "dev-1", eng2.DeviceID(), "existing engine not reconfigured")

	eng3, created3 := m.GetOrCreate("jb-2", "dev-2", "", nil)
	assert.True(t, created3)
	assert.NotEqual(t, eng, eng3)
}

func TestEngineManagerGetAndRemove(t *testing.T) {
	m := NewEngineManager()

	assert.Nil(t, m.Get("jb-1"))

	eng, _ := m.GetOrCreate("jb-1", "d", "", nil)
	assert.Equal(t, eng, m.Get("jb-1"))

	m.Remove("jb-1")
	assert.Nil(t, m.Get("jb-1"))

	m.Remove("jb-1") // double remove — no panic
}

func TestEngineManagerForEachAndStopAll(t *testing.T) {
	m := NewEngineManager()
	m.GetOrCreate("jb-1", "d", "", nil)
	m.GetOrCreate("jb-2", "d", "", nil)

	var ids []string
	m.ForEach(func(id string, eng *Engine) {
		ids = append(ids, id)
	})
	assert.ElementsMatch(t, []string{"jb-1", "jb-2"}, ids)

	m.StopAll() // must not panic with playing engines
}
