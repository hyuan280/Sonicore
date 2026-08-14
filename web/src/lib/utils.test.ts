import { describe, expect, it, beforeEach } from "vitest"
import {
  cn,
  formatDuration,
  formatFileSize,
  coverImageUrl,
  performerNames,
  parseLRC,
  findCurrentLine,
} from "./utils"

describe("cn", () => {
  it("merges class names", () => {
    expect(cn("a", "b")).toBe("a b")
  })

  it("filters falsy values", () => {
    expect(cn("a", false && "b", undefined, null, "c")).toBe("a c")
  })
})

describe("formatDuration", () => {
  it("formats zero", () => {
    expect(formatDuration(0)).toBe("0:00")
  })

  it("pads seconds", () => {
    expect(formatDuration(65)).toBe("1:05")
  })

  it("handles minutes above 59", () => {
    expect(formatDuration(3661)).toBe("61:01")
  })

  it("truncates fractional seconds", () => {
    expect(formatDuration(59.9)).toBe("0:59")
  })
})

describe("formatFileSize", () => {
  it("formats kilobytes", () => {
    expect(formatFileSize(512 * 1024)).toBe("512 KB")
  })

  it("formats megabytes with one decimal", () => {
    expect(formatFileSize(5 * 1024 * 1024)).toBe("5.0 MB")
  })
})

describe("coverImageUrl", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("includes session token and image id", () => {
    localStorage.setItem("session_token", "sess123")
    expect(coverImageUrl("img-1")).toBe("/api/c/sess123/img-1")
  })

  it("appends size param when given", () => {
    localStorage.setItem("session_token", "s")
    expect(coverImageUrl("img-1", 256)).toBe("/api/c/s/img-1?size=256")
  })

  it("leaves session empty when not stored", () => {
    expect(coverImageUrl("img-1")).toBe("/api/c//img-1")
  })
})

describe("performerNames", () => {
  it("joins performer names with slash", () => {
    const artists = [
      { artist_id: "1", name: "A", role: "performer" },
      { artist_id: "2", name: "B", role: "performer" },
    ]
    expect(performerNames(artists)).toBe("A/B")
  })

  it("excludes non-performer roles", () => {
    const artists = [
      { artist_id: "1", name: "A", role: "composer" },
      { artist_id: "2", name: "B", role: "performer" },
    ]
    expect(performerNames(artists)).toBe("B")
  })

  it("falls back to nested artist name", () => {
    const artists = [{ artist_id: "1", name: undefined as any, role: "performer", artist: { name: "Nested" } }]
    expect(performerNames(artists)).toBe("Nested")
  })

  it("returns empty for empty input", () => {
    expect(performerNames(undefined)).toBe("")
    expect(performerNames([])).toBe("")
  })
})

describe("parseLRC", () => {
  it("parses basic timestamps", () => {
    const lines = parseLRC("[00:12.34]hello\n[01:02.50]world")
    expect(lines).toEqual([
      { time: 12.34, text: "hello" },
      { time: 62.5, text: "world" },
    ])
  })

  it("supports colon and dot fraction separators with 2-3 digits", () => {
    const lines = parseLRC("[00:01:50]two-digit\n[00:02.050]three-digit")
    expect(lines).toEqual([
      { time: 1.5, text: "two-digit" },
      { time: 2.05, text: "three-digit" },
    ])
  })

  it("skips metadata and empty text lines", () => {
    const lines = parseLRC("[ar:artist]\n[ti:title]\n[00:10.00]\n[00:20.00]   lyric   ")
    expect(lines).toEqual([{ time: 20, text: "lyric" }])
  })

  it("sorts by time ascending", () => {
    const lines = parseLRC("[00:30.00]later\n[00:10.00]earlier")
    expect(lines.map(l => l.text)).toEqual(["earlier", "later"])
  })
})

describe("findCurrentLine", () => {
  const lines = [
    { time: 0, text: "a" },
    { time: 10, text: "b" },
    { time: 20, text: "c" },
  ]

  it("returns -1 for empty input", () => {
    expect(findCurrentLine([], 5)).toBe(-1)
  })

  it("returns -1 before first line", () => {
    expect(findCurrentLine(lines, -1)).toBe(-1)
  })

  it("picks the latest line at or before position", () => {
    expect(findCurrentLine(lines, 15)).toBe(1)
    expect(findCurrentLine(lines, 25)).toBe(2)
  })
})
