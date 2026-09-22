package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// listenProgressScript atomically maintains a per-listen "verified listened
// seconds" accumulator. It is the heart of the heat anti-abuse model: only
// progress that could actually have happened during the wall-clock elapsed
// since the previous update is credited, so the accumulator can never grow
// faster than real time.
//
// KEYS[1] = heat:listen:{user}:{track} (hash)
// ARGV[1] = candidate session token
// ARGV[2] = reported position (seconds)
// ARGV[3] = window (seconds; also the key TTL)
//
// Returns { sessionToken, listenedSeconds }.
//
// trusted = min(max(position-lastPos, 0), elapsed)
//
// A forward seek therefore contributes only the elapsed time, a pause or rewind
// contributes nothing, and starting/resuming from any position simply rebases
// the baseline without earning credit.
//
// The hash stores an "anchor" (window start). When now-anchor reaches the
// window, the script rotates the session token and resets the accumulator. This
// matters because the key TTL is refreshed on every report: a continuously
// reporting client would otherwise keep one session token forever, so after the
// independent count-gate key expired the database dedupe key would still match
// and the new play/completion would be dropped. Rotation keeps one session per
// window and lets repeat-one / long continuous listening count once per window.
var listenProgressScript = redis.NewScript(`
local key = KEYS[1]
local token = ARGV[1]
local pos = tonumber(ARGV[2])
local window = tonumber(ARGV[3])
local t = redis.call('TIME')
local now = tonumber(t[1])

if redis.call('EXISTS', key) == 0 then
  redis.call('HSET', key, 'token', token, 'lastPos', pos, 'lastUpdate', now, 'listened', 0, 'anchor', now)
  redis.call('EXPIRE', key, window)
  return {token, '0'}
end

local anchor = tonumber(redis.call('HGET', key, 'anchor'))
if not anchor then
  anchor = tonumber(redis.call('HGET', key, 'lastUpdate'))
end
if now - anchor >= window then
  redis.call('HSET', key, 'token', token, 'lastPos', pos, 'lastUpdate', now, 'listened', 0, 'anchor', now)
  redis.call('EXPIRE', key, window)
  return {token, '0'}
end

local storedToken = redis.call('HGET', key, 'token')
local lastPos = tonumber(redis.call('HGET', key, 'lastPos'))
local lastUpdate = tonumber(redis.call('HGET', key, 'lastUpdate'))
local listened = tonumber(redis.call('HGET', key, 'listened'))

local elapsed = now - lastUpdate
if elapsed < 0 then
  elapsed = 0
end
local advance = pos - lastPos
local trusted = 0
if advance > 0 then
  if advance <= elapsed then
    trusted = advance
  else
    trusted = elapsed
  end
end
listened = listened + trusted

redis.call('HSET', key, 'lastPos', pos, 'lastUpdate', now, 'listened', listened, 'anchor', anchor)
redis.call('EXPIRE', key, window)
return {storedToken, tostring(listened)}
`)

// PlayMarkerStore backs the heat anti-abuse controls. Keys contain no
// client-supplied identifier:
//
//	heat:listen:{user}:{track}  -> hash{token,lastPos,lastUpdate,listened,anchor}
//	heat:play:{user}:{track}    -> count gate for a play
//	heat:playc:{user}:{track}   -> count gate for a completion
//
// The listen hash accumulates verified listened seconds and rotates its session
// token each window (see listenProgressScript). The count gates use SET NX EX
// so a play and a completion are awarded at most once per track per window.
type PlayMarkerStore struct {
	vk *Valkey
}

func NewPlayMarkerStore(vk *Valkey) *PlayMarkerStore {
	return &PlayMarkerStore{vk: vk}
}

// ListenProgress credits the verified playback progress for this report and
// returns the listen's session token and the accumulated listened seconds. The
// token is stable within a window and rotates when the window elapses, so each
// window's play/completion is a distinct database dedupe key.
func (s *PlayMarkerStore) ListenProgress(ctx context.Context, userID, trackID, token string, position float64, window time.Duration) (string, float64, error) {
	if window <= 0 {
		window = time.Second
	}
	key := s.vk.key("heat:listen:" + userID + ":" + trackID)
	res, err := listenProgressScript.Run(ctx, s.vk.client, []string{key},
		token, position, int64(window.Seconds())).Result()
	if err != nil {
		return token, 0, err
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) < 2 {
		return token, 0, fmt.Errorf("unexpected listen progress result")
	}
	session, _ := arr[0].(string)
	if session == "" {
		session = token
	}
	listenedStr, _ := arr[1].(string)
	listened, _ := strconv.ParseFloat(listenedStr, 64)
	return session, listened, nil
}

// Allow reports whether a heat event of the given kind may be counted now for
// this user/track. token is stored as the marker value for diagnostics; it does
// not affect the gate.
func (s *PlayMarkerStore) Allow(ctx context.Context, kind, userID, trackID, token string, window time.Duration) (bool, error) {
	if window <= 0 {
		window = time.Second
	}
	key := s.vk.key("heat:" + kind + ":" + userID + ":" + trackID)
	return s.vk.client.SetNX(ctx, key, token, window).Result()
}
