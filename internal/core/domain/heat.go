package domain

import (
	"fmt"
	"time"
)

// Heat event types. Each maps to a track_events row; the sum of weights is the
// cached tracks.heat value.
//
// HeatEventDownload is awarded when an admin (or above) downloads a track's
// complete file. Its dedupe key is per (user, track), so each user is counted
// at most once per track regardless of how many times they download it.
const (
	HeatEventPlay         = "play"
	HeatEventPlayComplete = "play_complete"
	HeatEventFavorite     = "favorite"
	HeatEventPlaylistAdd  = "playlist_add"
	HeatEventDownload     = "download"
)

// Heat weights applied per action. Fixed by design (not configurable).
const (
	HeatWeightPlay         = 1
	HeatWeightPlayComplete = 1
	HeatWeightFavorite     = 5
	HeatWeightPlaylistAdd  = 2
	HeatWeightDownload     = 3
)

// A play only counts toward heat once the listener has accumulated at least
// HeatMinPlaySeconds seconds of verified playback or, for short tracks,
// HeatMinPlayRatio of the duration.
const (
	HeatMinPlaySeconds = 30.0
	HeatMinPlayRatio   = 0.5
)

// HeatCompleteRatio is the fraction of the track's duration that verified
// playback must cover to earn the extra play_complete weight. Option A:
// completion is based on trusted listened time, not on the reported position,
// so seeking to the end does not earn it.
const HeatCompleteRatio = 0.9

// Heat rate-limit tuning (see HeatRateLimitWindow).
const (
	// HeatPlayRateFloor is the minimum anti-abuse window, used for very short
	// tracks and unknown durations.
	HeatPlayRateFloor = 30 * time.Second
	// HeatPlayRateGrace keeps the window open slightly past the track's own
	// duration. Without it the marker would expire while a long track is still
	// playing, and a later ping would open a fresh window and count the same
	// listen again.
	HeatPlayRateGrace = 5 * time.Second
)

// heatMaxWindowSeconds caps the computed window. Beyond it the duration is
// clamped so the float->Duration conversion cannot overflow int64 and silently
// collapse the window to the floor.
const heatMaxWindowSeconds = 24 * 60 * 60

// HeatRateLimitWindow returns the Valkey anti-abuse window for one playback of
// a track: max(HeatPlayRateFloor, duration+HeatPlayRateGrace).
//
// This is intentionally larger than the track duration so a single continuous
// play is counted at most once: the listen marker stays alive for the whole
// track. A repeat-one loop restarts after roughly the track duration, so once
// the window elapses the listen marker rotates to a new session and the next
// loop can be counted again. There is no client-provided session id: the only
// inputs are the server-side track duration and the caller's identity, and the
// window bounds any abuse to at most one play (plus one completion) per track
// per window.
func HeatRateLimitWindow(durationSeconds float64) time.Duration {
	w := HeatPlayRateFloor
	if durationSeconds > 0 {
		if durationSeconds > heatMaxWindowSeconds {
			durationSeconds = heatMaxWindowSeconds
		}
		w = time.Duration(durationSeconds*float64(time.Second)) + HeatPlayRateGrace
	}
	if w < HeatPlayRateFloor {
		return HeatPlayRateFloor
	}
	return w
}

// Heat dedupe-key prefixes.
const (
	HeatKeyPrefixFavorite = "fav"
	HeatKeyPrefixPlaylist = "pl"
	HeatKeyPrefixPlay     = "play"
	HeatKeyPrefixDownload = "dl"
)

// FavoriteDedupeKey identifies the heat event for one user favoriting one track.
func FavoriteDedupeKey(userID, trackID string) string {
	return fmt.Sprintf("%s:%s:%s", HeatKeyPrefixFavorite, userID, trackID)
}

// PlaylistDedupeKey identifies the heat event for a track added to a playlist.
func PlaylistDedupeKey(playlistID, trackID string) string {
	return fmt.Sprintf("%s:%s:%s", HeatKeyPrefixPlaylist, playlistID, trackID)
}

// DownloadDedupeKey identifies the heat event for one user downloading one
// track. There is no session component: a user earns the download weight at
// most once per track, no matter how many times they download it.
func DownloadDedupeKey(userID, trackID string) string {
	return fmt.Sprintf("%s:%s:%s", HeatKeyPrefixDownload, userID, trackID)
}

// PlaySessionKey identifies one counted play. The session token is generated
// server-side when the Valkey rate limit allows a play, so no client input
// participates in the dedupe key. It keeps a single allowed play idempotent at
// the database layer alongside the rate limit.
func PlaySessionKey(userID, trackID, session string) string {
	return fmt.Sprintf("%s:%s:%s:%s", HeatKeyPrefixPlay, userID, trackID, session)
}

// PlayCompleteSessionKey identifies the completion bonus for one counted play.
// It is independent of PlaySessionKey so a completed listen can be awarded even
// when the play itself was already counted.
func PlayCompleteSessionKey(userID, trackID, session string) string {
	return fmt.Sprintf("playc:%s:%s:%s", userID, trackID, session)
}

// HeatQualifiesForListened reports whether the verified listened time is a
// meaningful play: at least HeatMinPlaySeconds seconds or, for short tracks,
// HeatMinPlayRatio of the duration. It is intentionally based on the trusted
// listened total (see cache.PlayMarkerStore) rather than a client-reported
// position, so resuming or seeking mid-track cannot be mistaken for forgery and
// a forged position cannot earn heat ahead of wall-clock time.
func HeatQualifiesForListened(listened, duration float64) bool {
	if listened >= HeatMinPlaySeconds {
		return true
	}
	return duration > 0 && listened/duration >= HeatMinPlayRatio
}

// HeatListenedIsComplete reports whether verified playback covered the whole
// track (HeatCompleteRatio of its duration). Option A: seeking to the end does
// not count as a completed listen; only accumulated real playback does.
func HeatListenedIsComplete(listened, duration float64) bool {
	return duration > 0 && listened/duration >= HeatCompleteRatio
}
