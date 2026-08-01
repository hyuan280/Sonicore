import { useCallback, useEffect, useRef, useState } from "react"

export interface MseBufferedRange {
  start: number
  end: number
}

export interface MseTrack {
  id: string
  duration: number
}

const CODEC_AAC = 'audio/mp4; codecs="mp4a.40.2"'
const CODEC_FLAC = 'audio/mp4; codecs="flac"'
const codecFor = (q: string) => (q === "lossless" ? CODEC_FLAC : CODEC_AAC)
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))
// Segments are requested in fixed, aligned blocks so URLs are deterministic
// and repeat plays hit the browser HTTP cache.
const BLOCK = 5
const ceilBlock = (t: number) => Math.ceil(t / BLOCK) * BLOCK
const floorGrid = (t: number, grid: number) => Math.floor(t / grid) * grid

/**
 * MediaSource-based playback. Segments are fetched from the server as
 * fragmented MP4 (init segment + time ranges) and appended to a SourceBuffer.
 *
 * Buffering strategy:
 *  - on start, fetch the first 5s so playback can begin;
 *  - while the playhead is under 30s, check once a second whether the buffer is
 *    at least 10s ahead; only when it falls behind, fetch 5s of data;
 *  - once the playhead passes 30s, fetch 20s of data per second until the track
 *    is fully buffered;
 *  - fetching pauses with playback; seeking to an unbuffered position quickly
 *    fills up to the target in 20s blocks.
 */
export function useMseAudio() {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [buffered, setBuffered] = useState<MseBufferedRange[]>([])
  const [waiting, setWaiting] = useState(true)
  const [error, setError] = useState(false)

  const msRef = useRef<MediaSource | null>(null)
  const sbRef = useRef<SourceBuffer | null>(null)
  const initRef = useRef(false)
  const busyRef = useRef(false)
  const trackRef = useRef<MseTrack | null>(null)
  const qualityRef = useRef("standard")
  const bufferedEndRef = useRef(0)
  // The aligned (multiple of BLOCK) end of the buffer. Kept as the floor of the
  // real buffered end because fragment durations make it drift past block
  // boundaries; requests always start at this boundary so URLs are deterministic.
  const alignedEnd = useCallback(() => {
    return Math.floor(bufferedEndRef.current / BLOCK) * BLOCK
  }, [])
  const versionRef = useRef(0)
  const clearWaitRef = useRef<Promise<void> | null>(null)
  const objectUrlRef = useRef<string | null>(null)
  const stallRef = useRef(0)
  const errorRef = useRef(0)
  const playingRef = useRef(false)

  const refreshBuffered = useCallback(() => {
    const sb = sbRef.current
    const el = audioRef.current
    if (!sb || !el) {
      setBuffered([])
      bufferedEndRef.current = 0
      return
    }
    const ranges: MseBufferedRange[] = []
    let end = 0
    for (let i = 0; i < sb.buffered.length; i++) {
      const s = sb.buffered.start(i)
      const e = sb.buffered.end(i)
      ranges.push({ start: s, end: e })
      if (e > end) end = e
    }
    bufferedEndRef.current = end
    setBuffered(ranges)
  }, [])

  const segmentUrl = useCallback((start: number, dur: number) => {
    const t = trackRef.current
    const session = localStorage.getItem("session_token")
    if (!t || !session) return ""
    return `/api/s/${session}/${t.id}?quality=${qualityRef.current}&start=${start.toFixed(3)}&duration=${dur.toFixed(3)}`
  }, [])

  const initUrl = useCallback(() => {
    const t = trackRef.current
    const session = localStorage.getItem("session_token")
    if (!t || !session) return ""
    return `/api/s/${session}/${t.id}?quality=${qualityRef.current}&init=1`
  }, [])

  const appendBytes = useCallback((bytes: ArrayBuffer): Promise<void> => {
    const sb = sbRef.current
    if (!sb) return Promise.resolve()
    if (clearWaitRef.current) return clearWaitRef.current.then(() => appendBytes(bytes))
    return new Promise<void>((resolve, reject) => {
      const onEnd = () => { sb.removeEventListener("updateend", onEnd); resolve() }
      const onErr = () => { sb.removeEventListener("error", onErr); reject(new Error("sourcebuffer append failed")) }
      sb.addEventListener("updateend", onEnd)
      sb.addEventListener("error", onErr)
      try {
        sb.appendBuffer(bytes)
      } catch (e) {
        sb.removeEventListener("updateend", onEnd)
        reject(e)
      }
    })
  }, [])

  const clearBuffer = useCallback(() => {
    const sb = sbRef.current
    if (!sb || sb.buffered.length === 0) {
      clearWaitRef.current = null
      return Promise.resolve()
    }
    const end = sb.buffered.end(sb.buffered.length - 1)
    const p = new Promise<void>((resolve) => {
      const onEnd = () => { sb.removeEventListener("updateend", onEnd); resolve() }
      sb.addEventListener("updateend", onEnd)
      try {
        sb.remove(0, end)
      } catch {
        resolve()
      }
    })
    clearWaitRef.current = p
    return p
  }, [])

  const fetchSegment = useCallback(async (start: number, dur: number) => {
    const url = segmentUrl(start, dur)
    if (!url) throw new Error("no session")
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), 30000)
    try {
      const res = await fetch(url, { signal: ctrl.signal })
      if (!res.ok) throw new Error(`segment ${res.status}`)
      return await res.arrayBuffer()
    } finally {
      clearTimeout(timer)
    }
  }, [segmentUrl])

  // Fetch and append a single chunk of `chunk` seconds. The request starts at
  // the buffer end aligned down to the chunk grid (stable across plays), unless
  // an explicit start is given (used by seek fills to continue from the real
  // buffer end and avoid re-downloading already-buffered data).
  const fetchChunk = useCallback(async (chunk: number, startOverride?: number) => {
    const start = startOverride !== undefined ? startOverride : floorGrid(alignedEnd(), chunk)
    const before = bufferedEndRef.current
    const bytes = await fetchSegment(start, chunk)
    await appendBytes(bytes)
    refreshBuffered()
    if (bufferedEndRef.current <= before + 0.05) {
      stallRef.current++
      if (stallRef.current >= 3) {
        console.error("[mse] buffer not advancing, stopping")
        setError(true)
        throw new Error("buffer stalled")
      }
      await sleep(1000)
    } else {
      stallRef.current = 0
    }
  }, [alignedEnd, appendBytes, fetchSegment, refreshBuffered])

  // Per-second strategy driver (gated on playback state).
  const tick = useCallback(async () => {
    const el = audioRef.current
    const t = trackRef.current
    if (!el || !t || !initRef.current || busyRef.current || errorRef.current) return
    if (!playingRef.current) return

    const ms = msRef.current
    const duration = t.duration || 0
    const playhead = el.currentTime
    const buffered = bufferedEndRef.current

    if (buffered >= duration - 0.05 && duration > 0) {
      if (ms && ms.readyState === "open") {
        try { ms.endOfStream() } catch {}
      }
      return
    }

    const end = alignedEnd()
    let chunk: number
    if (end <= 0.01) {
      chunk = BLOCK
    } else if (playhead < 30) {
      // keep ~10s ahead of the playhead; only request 5s when it falls behind
      if (end < playhead + 10) {
        chunk = BLOCK
      } else {
        return
      }
    } else {
      chunk = 20
    }
    if (end + chunk <= buffered + 0.05) return

    const ver = versionRef.current
    busyRef.current = true
    try {
      await fetchChunk(chunk)
    } catch (e) {
      console.warn("[mse] buffer error:", e)
      if (versionRef.current === ver && !errorRef.current) {
        errorRef.current++
        if (errorRef.current >= 3) {
          setError(true)
        } else {
          await sleep(1000)
        }
      }
    } finally {
      busyRef.current = false
    }
  }, [alignedEnd, fetchChunk])

  // Quickly fill the buffer up to the target area with rapid back-to-back
  // requests (not gated by the one-per-second interval).
  const fastFillTo = useCallback(async (target: number) => {
    const t = trackRef.current
    if (!t || !initRef.current || busyRef.current || errorRef.current) return
    const duration = t.duration || 0
    const targetEnd = Math.min(ceilBlock(target) + BLOCK, duration || ceilBlock(target) + BLOCK)
    if (targetEnd <= alignedEnd() + 0.05) {
      tick()
      return
    }
    const ver = versionRef.current
    busyRef.current = true
    setWaiting(true)
    try {
      while (alignedEnd() < targetEnd && versionRef.current === ver && !errorRef.current) {
        await fetchChunk(20, alignedEnd())
      }
    } catch (e) {
      console.warn("[mse] fill error:", e)
      if (versionRef.current === ver && !errorRef.current) {
        errorRef.current++
        if (errorRef.current >= 3) {
          setError(true)
        }
      }
    } finally {
      busyRef.current = false
    }
    if (versionRef.current === ver && !errorRef.current) tick()
  }, [alignedEnd, fetchChunk, tick])

  // Called when playback starts: if the playhead is beyond the buffered data
  // (e.g. a seek made while paused), fill to it quickly; otherwise run the
  // normal strategy.
  const kickBuffering = useCallback(() => {
    const el = audioRef.current
    if (!el) return
    if (el.currentTime > bufferedEndRef.current + 0.1) {
      fastFillTo(el.currentTime)
    } else {
      tick()
    }
  }, [fastFillTo, tick])

  const seek = useCallback((time: number) => {
    const el = audioRef.current
    if (!el || !trackRef.current) return
    const sb = sbRef.current
    const startOfBuffer = sb && sb.buffered.length > 0 ? sb.buffered.start(0) : Infinity
    const bufferedEnd = bufferedEndRef.current
    if (sb && time < startOfBuffer - 0.1) {
      // seek back before the current buffer: clear and refill from the target
      setWaiting(true)
      clearBuffer().then(() => {
        bufferedEndRef.current = 0
        setBuffered([])
        try { el.currentTime = Math.max(0, time) } catch {}
        if (playingRef.current) fastFillTo(time)
      })
    } else if (sb && time > bufferedEnd + 0.1) {
      // seek forward past the buffer: seek now, fill quickly only while playing
      try { el.currentTime = Math.max(0, time) } catch {}
      if (playingRef.current) fastFillTo(time)
    } else {
      try { el.currentTime = Math.max(0, time) } catch {}
    }
  }, [clearBuffer, fastFillTo])

  const start = useCallback((track: MseTrack, quality: string, resumeAt = 0, shouldPlay = false) => {
    const el = audioRef.current
    if (!el) return
    versionRef.current++
    const ver = versionRef.current
    trackRef.current = { id: track.id, duration: track.duration || 0 }
    qualityRef.current = quality
    initRef.current = false
    busyRef.current = false
    playingRef.current = false
    clearWaitRef.current = null
    stallRef.current = 0
    errorRef.current = 0
    setError(false)
    bufferedEndRef.current = 0
    setBuffered([])
    setWaiting(true)

    if (objectUrlRef.current) {
      URL.revokeObjectURL(objectUrlRef.current)
      objectUrlRef.current = null
    }

    if (!("MediaSource" in window)) return

    const ms = new MediaSource()
    msRef.current = ms
    const url = URL.createObjectURL(ms)
    objectUrlRef.current = url
    el.src = url

    ms.addEventListener("sourceopen", () => {
      if (versionRef.current !== ver) return
      if (ms.sourceBuffers.length === 0) {
        try { ms.addSourceBuffer(codecFor(quality)) } catch { return }
      }
      sbRef.current = ms.sourceBuffers[0]
      const ctrl = new AbortController()
      const timer = setTimeout(() => ctrl.abort(), 30000)
      fetch(initUrl(), { signal: ctrl.signal })
        .then(r => { if (!r.ok) throw new Error(`init ${r.status}`); return r.arrayBuffer() })
        .then(bytes => appendBytes(bytes))
        .then(() => {
          if (versionRef.current !== ver) return
          initRef.current = true
          if (track.duration > 0 && ms.readyState === "open") {
            try { ms.duration = track.duration } catch {}
          }
          refreshBuffered()
          if (resumeAt > 0) {
            try { el.currentTime = resumeAt } catch {}
            fastFillTo(resumeAt)
          } else {
            kickBuffering()
          }
          if (shouldPlay) {
            el.play().catch(() => {})
          }
        })
        .catch(e => {
          console.warn("[mse] init error:", e)
          setError(true)
        })
        .finally(() => clearTimeout(timer))
    })
  }, [appendBytes, fastFillTo, initUrl, kickBuffering, refreshBuffered])

  const stop = useCallback(() => {
    versionRef.current++
    const el = audioRef.current
    if (el) {
      try { el.src = "" } catch {}
    }
    if (objectUrlRef.current) {
      URL.revokeObjectURL(objectUrlRef.current)
      objectUrlRef.current = null
    }
    msRef.current = null
    sbRef.current = null
    initRef.current = false
    playingRef.current = false
    trackRef.current = null
    bufferedEndRef.current = 0
    setBuffered([])
  }, [])

  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    const onWaiting = () => setWaiting(true)
    const onPlaying = () => { setWaiting(false); tick() }
    const onPlay = () => {
      playingRef.current = true
      kickBuffering()
    }
    const onPause = () => { playingRef.current = false }
    const onSeeked = () => tick()
    el.addEventListener("waiting", onWaiting)
    el.addEventListener("playing", onPlaying)
    el.addEventListener("play", onPlay)
    el.addEventListener("pause", onPause)
    el.addEventListener("seeked", onSeeked)
    const id = setInterval(() => { tick() }, 1000)
    return () => {
      clearInterval(id)
      el.removeEventListener("waiting", onWaiting)
      el.removeEventListener("playing", onPlaying)
      el.removeEventListener("play", onPlay)
      el.removeEventListener("pause", onPause)
      el.removeEventListener("seeked", onSeeked)
    }
  }, [kickBuffering, tick])

  return { audioRef, buffered, waiting, error, start, stop, seek }
}
