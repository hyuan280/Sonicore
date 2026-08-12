import { describe, expect, it, beforeEach } from "vitest"
import {
  DEFAULT_LYRICS_SETTINGS,
  LYRICS_SETTINGS_KEY,
  loadLyricsSettings,
  saveLyricsSettings,
} from "./lyricsSettings"

describe("lyricsSettings", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("returns defaults when nothing stored", () => {
    expect(loadLyricsSettings()).toEqual(DEFAULT_LYRICS_SETTINGS)
  })

  it("round-trips saved settings", () => {
    const settings = { opacity: 0.8, activeColor: "#ffffff", fontSize: 32 }
    saveLyricsSettings(settings)
    expect(loadLyricsSettings()).toEqual(settings)
  })

  it("merges partial stored settings over defaults", () => {
    localStorage.setItem(LYRICS_SETTINGS_KEY, JSON.stringify({ opacity: 0.5 }))
    expect(loadLyricsSettings()).toEqual({ ...DEFAULT_LYRICS_SETTINGS, opacity: 0.5 })
  })

  it("falls back to defaults on corrupt JSON", () => {
    localStorage.setItem(LYRICS_SETTINGS_KEY, "{not-json")
    expect(loadLyricsSettings()).toEqual(DEFAULT_LYRICS_SETTINGS)
  })
})
