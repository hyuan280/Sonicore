import { describe, expect, it, beforeEach } from "vitest"
import { streamInitUrl, codecFor } from "./useMseAudio"

describe("streamInitUrl", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("builds init URL with session token and quality", () => {
    localStorage.setItem("session_token", "sess")
    expect(streamInitUrl("t1", "standard")).toBe("/api/s/sess/t1?quality=standard&init=1")
    expect(streamInitUrl("t1", "lossless")).toBe("/api/s/sess/t1?quality=lossless&init=1")
  })

  it("returns empty string without a session token", () => {
    expect(streamInitUrl("t1", "standard")).toBe("")
  })

  it("returns empty string without a track id", () => {
    localStorage.setItem("session_token", "sess")
    expect(streamInitUrl("", "standard")).toBe("")
  })
})

describe("codecFor", () => {
  it("maps lossless to FLAC codec", () => {
    expect(codecFor("lossless")).toBe('audio/mp4; codecs="flac"')
  })

  it("maps everything else to AAC codec", () => {
    expect(codecFor("standard")).toBe('audio/mp4; codecs="mp4a.40.2"')
    expect(codecFor("high")).toBe('audio/mp4; codecs="mp4a.40.2"')
  })
})
