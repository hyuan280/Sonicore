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
export const codecFor = (q: string) => (q === "lossless" ? CODEC_FLAC : CODEC_AAC)
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))
const BLOCK = 5
const ceilBlock = (t: number) => Math.ceil(t / BLOCK) * BLOCK
const floorGrid = (t: number, grid: number) => Math.floor(t / grid) * grid

class SbError extends Error {
  constructor(msg: string) {
    super(msg)
    this.name = "SbError"
  }
}

export function streamInitUrl(trackId: string, quality: string): string {
  const session = localStorage.getItem("session_token")
  if (!session || !trackId) return ""
  return `/api/s/${session}/${trackId}?quality=${quality}&init=1`
}

export function useMseAudio(onFatal: () => void) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [buffered, setBuffered] = useState<MseBufferedRange[]>([])
  const [waiting, setWaiting] = useState(true)

  const msRef = useRef<MediaSource | null>(null)
  const sbRef = useRef<SourceBuffer | null>(null)
  const initRef = useRef(false)
  const busyRef = useRef(false)
  const trackRef = useRef<MseTrack | null>(null)
  const qualityRef = useRef("standard")
  const bufferedEndRef = useRef(0)
  const alignedEnd = useCallback(() => {
    return Math.floor(bufferedEndRef.current / BLOCK) * BLOCK
  }, [])
  const versionRef = useRef(0)
  const clearWaitRef = useRef<Promise<void> | null>(null)
  const objectUrlRef = useRef<string | null>(null)
  const stallRef = useRef(0)
  const errorRef = useRef(0)
  const playingRef = useRef(false)
  const fatalRef = useRef(onFatal)
  const quotaHitRef = useRef(false)
  const removeWaitRef = useRef<Promise<void> | null>(null)

  useEffect(() => {
    fatalRef.current = onFatal
  })

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
    const ver = versionRef.current
    if (clearWaitRef.current) {
      return clearWaitRef.current.then(() => {
        if (versionRef.current !== ver) return
        return appendBytes(bytes)
      })
    }
    if (sb.updating) {
      return new Promise<void>((resolve, reject) => {
        let settled = false
        const rej = (msg: string) => {
          if (settled) return
          settled = true
          sb.removeEventListener("updateend", onEnd)
          sb.removeEventListener("error", onErr)
          clearTimeout(timer)
          const err = new SbError(msg)
          reject(err)
        }
        const succeed = () => {
          if (settled) return
          settled = true
          sb.removeEventListener("updateend", onEnd)
          sb.removeEventListener("error", onErr)
          clearTimeout(timer)
          if (versionRef.current !== ver) return resolve()
          appendBytes(bytes).then(resolve).catch(reject)
        }
        const onEnd = () => succeed()
        const onErr = () => rej("sourcebuffer error while waiting")
        const timer = setTimeout(() => { try { sb.abort() } catch {}; rej("sourcebuffer wait timed out") }, 10000)
        sb.addEventListener("updateend", onEnd)
        sb.addEventListener("error", onErr)
      })
    }
    return new Promise<void>((resolve, reject) => {
      let settled = false
      const rej = (msg: string) => {
        if (settled) return
        settled = true
        sb.removeEventListener("updateend", onEnd)
        sb.removeEventListener("error", onErr)
        clearTimeout(timer)
        const err = new SbError(msg)
        reject(err)
      }
      const onEnd = () => {
        if (settled) return
        settled = true
        sb.removeEventListener("updateend", onEnd)
        sb.removeEventListener("error", onErr)
        clearTimeout(timer)
        resolve()
      }
      const onErr = () => rej("sourcebuffer append failed")
      const timer = setTimeout(() => { try { sb.abort() } catch {}; rej("sourcebuffer append timed out") }, 10000)
      sb.addEventListener("updateend", onEnd)
      sb.addEventListener("error", onErr)
      try {
        sb.appendBuffer(bytes)
      } catch (e) {
        sb.removeEventListener("updateend", onEnd)
        sb.removeEventListener("error", onErr)
        clearTimeout(timer)
        reject(e)
      }
    })
  }, [])

  const clearBuffer = useCallback((): Promise<void> => {
    const sb = sbRef.current
    if (!sb || sb.buffered.length === 0) {
      clearWaitRef.current = null
      return Promise.resolve()
    }
    if (sb.updating) {
      const wait = new Promise<void>(resolve => {
        let settled = false
        const done = () => {
          if (settled) return
          settled = true
          sb.removeEventListener("updateend", onEnd)
          sb.removeEventListener("error", onErr)
          clearTimeout(timer)
          resolve()
        }
        const onEnd = () => done()
        const onErr = () => done()
        const timer = setTimeout(() => done(), 5000)
        sb.addEventListener("updateend", onEnd)
        sb.addEventListener("error", onErr)
      })
      const retry = (): Promise<void> => {
        if (clearWaitRef.current === chained) {
          clearWaitRef.current = null
        }
        const sb2 = sbRef.current
        if (!sb2 || sb2.buffered.length === 0) {
          return Promise.resolve()
        }
        if (sb2.updating) {
          try { sb2.abort() } catch {}
        }
        if (!sb2 || sb2.buffered.length === 0) {
          return Promise.resolve()
        }
        const end = sb2.buffered.end(sb2.buffered.length - 1)
        const p = new Promise<void>((resolve) => {
          const onEnd2 = () => { sb2.removeEventListener("updateend", onEnd2); resolve() }
          sb2.addEventListener("updateend", onEnd2)
          try {
            sb2.remove(0, end)
          } catch {
            sb2.removeEventListener("updateend", onEnd2)
            resolve()
          }
        })
        const chained2 = p.then(() => { if (clearWaitRef.current === chained2) clearWaitRef.current = null })
        clearWaitRef.current = chained2
        return chained2
      }
      const chained: Promise<void> = wait.then(retry)
      clearWaitRef.current = chained
      return chained
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
    const chained = p.then(() => { if (clearWaitRef.current === chained) clearWaitRef.current = null })
    clearWaitRef.current = chained
    return chained
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

  const fetchChunk = useCallback(async (chunk: number, startOverride?: number) => {
    const start = startOverride !== undefined ? startOverride : floorGrid(alignedEnd(), chunk)
    const before = bufferedEndRef.current
    const ver = versionRef.current
    try {
      const bytes = await fetchSegment(start, chunk)
      if (versionRef.current !== ver) return true
      await appendBytes(bytes)
      if (versionRef.current !== ver) return true
      refreshBuffered()
    } catch (e: unknown) {
      const errName = (e as { name?: string })?.name
      if (errName === "QuotaExceededError") {
        quotaHitRef.current = true
        console.warn("[mse] buffer quota exceeded, pausing buffering")
        return false
      }
      if (errName === "InvalidStateError") {
        const ms2 = msRef.current
        if (ms2 && ms2.readyState === "ended") return false
      }
      if (e instanceof SbError) {
        errorRef.current++
        console.warn("[mse] sourcebuffer error, retrying")
        if (errorRef.current >= 4) throw e
        return false
      }
      throw e
    }
    if (bufferedEndRef.current <= before + 0.05) {
      const t = trackRef.current
      if (t && t.duration > 0 && before >= t.duration - 1) {
        const ms = msRef.current
        if (ms && ms.readyState === "open") {
          try { ms.endOfStream() } catch {}
        }
        stallRef.current = 0
        errorRef.current = 0
        return true
      }
      stallRef.current++
      if (stallRef.current >= 6) {
        console.warn("[mse] buffer stalled, retrying")
        stallRef.current = 0
        throw new Error("buffer stalled")
      }
      await sleep(2000)
    } else {
      stallRef.current = 0
      errorRef.current = 0
    }
    return true
  }, [alignedEnd, appendBytes, fetchSegment, refreshBuffered])

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

  const handleChunkError = useCallback(async (ver: number) => {
    if (versionRef.current !== ver) return
    errorRef.current++
    if (errorRef.current >= 4) {
      stop()
      fatalRef.current()
    } else {
      await sleep(1000)
    }
  }, [stop])

  const trimPlayed = useCallback(() => {
    const sb = sbRef.current
    const el = audioRef.current
    if (!sb || !el || sb.buffered.length === 0) return
    if (removeWaitRef.current) return
    if (clearWaitRef.current) return
    const margin = quotaHitRef.current ? 30 : 60
    const currentTime = el.currentTime
    if (currentTime < margin) return
    const bufEnd = sb.buffered.end(sb.buffered.length - 1)
    const bufStart = sb.buffered.start(0)
    const totalBuffered = bufEnd - bufStart
    const minBuffer = quotaHitRef.current ? 60 : 120
    if (totalBuffered < minBuffer) return
    for (let i = 0; i < sb.buffered.length; i++) {
      const s = sb.buffered.start(i)
      const e = sb.buffered.end(i)
      if (s < currentTime - margin) {
        const ver = versionRef.current
        const removeEnd = Math.min(e, currentTime - margin)
        try {
          sb.remove(s, removeEnd)
          const p = new Promise<void>(resolve => {
            let settled = false
            const done = () => {
              if (settled) return
              settled = true
              sb.removeEventListener("updateend", onEnd)
              sb.removeEventListener("error", onErr)
              clearTimeout(timer)
              resolve()
            }
            const onEnd = () => done()
            const onErr = () => done()
            const timer = setTimeout(() => { try { sb.abort() } catch {}; done() }, 5000)
            sb.addEventListener("updateend", onEnd)
            sb.addEventListener("error", onErr)
          })
          removeWaitRef.current = p.then(() => {
            removeWaitRef.current = null
            if (versionRef.current === ver) refreshBuffered()
          })
        } catch {
          console.warn("[mse] trim remove failed (sourcebuffer busy)")
        }
        break
      }
    }
  }, [refreshBuffered])

  const tick = useCallback(async () => {
    const el = audioRef.current
    const t = trackRef.current
    if (!el || !t || !initRef.current || busyRef.current) return
    if (!playingRef.current) return

    if (quotaHitRef.current) {
      const sb = sbRef.current
      if (sb && sb.buffered.length > 0) {
        const bufEnd = sb.buffered.end(sb.buffered.length - 1)
        const ahead = bufEnd - el.currentTime
        if (ahead < 10) {
          quotaHitRef.current = false
          refreshBuffered()
        } else {
          trimPlayed()
          return
        }
      } else {
        quotaHitRef.current = false
      }
    }

    trimPlayed()

    const ms = msRef.current
    const duration = t.duration || 0
    const playhead = el.currentTime
    const buf = bufferedEndRef.current

    if (ms && ms.readyState === "ended") return

    if (buf >= duration - 0.05 && duration > 0) {
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
      if (end < playhead + 10) {
        chunk = BLOCK
      } else {
        return
      }
    } else {
      chunk = 20
    }
    if (end + chunk <= buf + 0.05) return

    const ver = versionRef.current
    busyRef.current = true
    try {
      const ok = await fetchChunk(chunk)
      if (!ok) return
    } catch (e) {
      console.warn("[mse] buffer error:", e)
      if (versionRef.current === ver) await handleChunkError(ver)
    } finally {
      busyRef.current = false
    }
  }, [alignedEnd, fetchChunk, handleChunkError, trimPlayed])

  const fastFillTo = useCallback(async (target: number) => {
    const t = trackRef.current
    if (!t || !initRef.current || busyRef.current) return
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
      while (alignedEnd() < targetEnd && versionRef.current === ver && !errorRef.current && !quotaHitRef.current) {
        const ok = await fetchChunk(20, alignedEnd())
        if (!ok) break
      }
    } catch (e) {
      console.warn("[mse] fill error:", e)
      await handleChunkError(ver)
    } finally {
      busyRef.current = false
    }
    if (versionRef.current === ver && !errorRef.current) tick()
  }, [alignedEnd, fetchChunk, handleChunkError, tick])

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
    if (!el || !trackRef.current || !initRef.current) return
    versionRef.current++
    const sb = sbRef.current
    const startOfBuffer = sb && sb.buffered.length > 0 ? sb.buffered.start(0) : Infinity
    const bufEnd = bufferedEndRef.current
    if (sb && time < startOfBuffer - 0.1) {
      setWaiting(true)
      clearBuffer().then(() => {
        bufferedEndRef.current = 0
        quotaHitRef.current = false
        removeWaitRef.current = null
        setBuffered([])
        try { el.currentTime = Math.max(0, time) } catch {}
        if (playingRef.current) fastFillTo(time)
      })
    } else if (sb && time > bufEnd + 0.1) {
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
    quotaHitRef.current = false
    removeWaitRef.current = null
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

      const initOnce = async () => {
        if (versionRef.current !== ver) return

        const ctrl = new AbortController()
        const timer = setTimeout(() => ctrl.abort(), 10000)
        try {
          const res = await fetch(initUrl(), { signal: ctrl.signal })
          clearTimeout(timer)
          if (!res.ok) throw new Error(`init ${res.status}`)
          const bytes = await res.arrayBuffer()
          if (versionRef.current !== ver) return
          await appendBytes(bytes)

          if (versionRef.current !== ver) return
          initRef.current = true
          setWaiting(false)
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
        } catch (e) {
          clearTimeout(timer)
          console.warn("[mse] init error:", e)
          if (versionRef.current !== ver) return
          stop()
          setWaiting(false)
          fatalRef.current()
        }
      }

      initOnce()
    })
  }, [appendBytes, fastFillTo, initUrl, kickBuffering, refreshBuffered, stop])

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
    const onOnline = () => {
      if (errorRef.current > 0) {
        errorRef.current = 0
      }
    }
    el.addEventListener("waiting", onWaiting)
    el.addEventListener("playing", onPlaying)
    el.addEventListener("play", onPlay)
    el.addEventListener("pause", onPause)
    el.addEventListener("seeked", onSeeked)
    window.addEventListener("online", onOnline)
    const id = setInterval(() => { tick() }, 1000)
    return () => {
      clearInterval(id)
      window.removeEventListener("online", onOnline)
      el.removeEventListener("waiting", onWaiting)
      el.removeEventListener("playing", onPlaying)
      el.removeEventListener("play", onPlay)
      el.removeEventListener("pause", onPause)
      el.removeEventListener("seeked", onSeeked)
    }
  }, [kickBuffering, tick])

  return { audioRef, buffered, waiting, start, stop, seek }
}
