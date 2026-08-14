import { describe, expect, it, beforeEach, vi, afterEach } from "vitest"
import {
  mediaSessionSupported,
  setMediaSessionMetadata,
  setMediaSessionPlaybackState,
  setMediaSessionPositionState,
  bindMediaSessionActions,
  clearMediaSessionActions,
} from "./mediaSession"
import type { PlayerTrack } from "../stores/player"

const msMock = {
  metadata: null as any,
  playbackState: "",
  setActionHandler: vi.fn(),
  setPositionState: vi.fn(),
}

class MediaMetadataStub {
  data: any
  constructor(data: any) {
    this.data = data
  }
}

function track(partial: Partial<PlayerTrack> = {}): PlayerTrack {
  return {
    id: "t1", title: "Song", album: "Album", duration: 200, suffix: "mp3",
    artists: [{ artist_id: "a1", name: "Artist", role: "performer" }],
    ...partial,
  }
}

describe("mediaSession", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, "mediaSession", {
      configurable: true,
      value: msMock,
    })
    vi.stubGlobal("MediaMetadata", MediaMetadataStub)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  describe("mediaSessionSupported", () => {
    it("returns false when mediaSession is unavailable", () => {
      delete (navigator as any).mediaSession
      expect(mediaSessionSupported()).toBe(false)
    })

    it("returns true when mediaSession exists", () => {
      expect(mediaSessionSupported()).toBe(true)
    })
  })

  describe("setMediaSessionMetadata", () => {
    it("builds metadata with artist and artwork for track with cover", () => {
      localStorage.setItem("session_token", "sess")
      setMediaSessionMetadata(track({ cover_image_id: "img1" }))
      const data = msMock.metadata.data
      expect(data.title).toBe("Song")
      expect(data.artist).toBe("Artist")
      expect(data.album).toBe("Album")
      expect(data.artwork).toEqual([
        { src: expect.stringContaining("/api/c/sess/img1?size=512"), sizes: "512x512", type: "image/jpeg" },
      ])
    })

    it("omits artwork when track has no cover", () => {
      setMediaSessionMetadata(track())
      expect(msMock.metadata.data.artwork).toEqual([])
    })

    it("joins multiple performers with slash", () => {
      setMediaSessionMetadata(track({
        artists: [
          { artist_id: "a1", name: "A", role: "performer" },
          { artist_id: "a2", name: "B", role: "performer" },
        ],
      }))
      expect(msMock.metadata.data.artist).toBe("A/B")
    })

    it("clears metadata for null track", () => {
      setMediaSessionMetadata(null)
      expect(msMock.metadata).toBeNull()
    })

    it("is a no-op when unsupported", () => {
      delete (navigator as any).mediaSession
      expect(() => setMediaSessionMetadata(track())).not.toThrow()
    })
  })

  describe("setMediaSessionPlaybackState", () => {
    it("sets playback state", () => {
      setMediaSessionPlaybackState("playing")
      expect(msMock.playbackState).toBe("playing")
    })
  })

  describe("setMediaSessionPositionState", () => {
    it("passes position clamped to duration", () => {
      setMediaSessionPositionState(250, 200)
      expect(msMock.setPositionState).toHaveBeenCalledWith({ duration: 200, position: 200, playbackRate: 1 })
    })

    it("passes position as-is when within range", () => {
      setMediaSessionPositionState(50, 200, 2)
      expect(msMock.setPositionState).toHaveBeenCalledWith({ duration: 200, position: 50, playbackRate: 2 })
    })

    it("skips invalid durations", () => {
      setMediaSessionPositionState(50, 0)
      expect(msMock.setPositionState).not.toHaveBeenCalled()
      setMediaSessionPositionState(50, Number.NaN)
      expect(msMock.setPositionState).not.toHaveBeenCalled()
    })
  })

  describe("bindMediaSessionActions", () => {
    it("registers all provided handlers", () => {
      const handlers = {
        play: () => {}, pause: () => {}, next: () => {}, prev: () => {},
        seekTo: () => {},
      }
      bindMediaSessionActions(handlers)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("play", handlers.play)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("pause", handlers.pause)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("nexttrack", handlers.next)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("previoustrack", handlers.prev)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("seekto", expect.any(Function))
    })

    it("registers null for missing handlers", () => {
      bindMediaSessionActions({})
      expect(msMock.setActionHandler).toHaveBeenCalledWith("play", null)
      expect(msMock.setActionHandler).toHaveBeenCalledWith("seekto", null)
    })

    it("seekto handler forwards seekTime only", () => {
      let received: number | undefined
      bindMediaSessionActions({ seekTo: (t) => { received = t } })
      const seekHandler = msMock.setActionHandler.mock.calls.find(([a]) => a === "seekto")![1]
      seekHandler({ seekTime: 42, fastSeek: true })
      expect(received).toBe(42)
    })

    it("is a no-op when unsupported", () => {
      delete (navigator as any).mediaSession
      expect(() => bindMediaSessionActions({})).not.toThrow()
    })
  })

  describe("clearMediaSessionActions", () => {
    it("clears all five actions", () => {
      clearMediaSessionActions()
      for (const action of ["play", "pause", "nexttrack", "previoustrack", "seekto"]) {
        expect(msMock.setActionHandler).toHaveBeenCalledWith(action, null)
      }
    })
  })
})
