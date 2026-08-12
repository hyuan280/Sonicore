import { describe, expect, it, beforeEach, vi } from "vitest"

vi.mock("../api/client", () => ({
  api: {
    jukebox: { list: vi.fn(), create: vi.fn(), delete: vi.fn() },
  },
}))

import { api } from "../api/client"
import { useJukebox } from "./jukebox"
import type { JukeboxInfo } from "./jukebox"

const mocked = vi.mocked(api.jukebox)

function box(id: string, extra: Partial<JukeboxInfo> = {}): JukeboxInfo {
  return {
    id, name: id, device_id: "", device_name: "", volume: 50,
    play_mode: "normal", queue: [], queue_idx: 0,
    shuffle_order: [], shuffle_idx: 0, is_playing: false,
    current_track_id: "", path_mapping: {},
    created_at: "", updated_at: "", ...extra,
  }
}

describe("useJukebox", () => {
  beforeEach(() => {
    useJukebox.setState({ list: [], loading: false })
    vi.clearAllMocks()
  })

  it("loads jukebox list", async () => {
    const boxes = [box("j1", { name: "Living Room" })]
    mocked.list.mockResolvedValue({ jukeboxes: boxes })
    await useJukebox.getState().loadList()
    expect(useJukebox.getState().list).toEqual(boxes)
    expect(useJukebox.getState().loading).toBe(false)
  })

  it("clears loading on failure and keeps list", async () => {
    mocked.list.mockRejectedValue(new Error("boom"))
    await useJukebox.getState().loadList()
    expect(useJukebox.getState().loading).toBe(false)
    expect(useJukebox.getState().list).toEqual([])
  })

  it("appends created jukebox", async () => {
    const j = box("j2", { name: "Kitchen" })
    mocked.create.mockResolvedValue(j)
    const res = await useJukebox.getState().create("Kitchen", "cfg1")
    expect(res).toEqual(j)
    expect(mocked.create).toHaveBeenCalledWith({ name: "Kitchen", device_config_id: "cfg1" })
    expect(useJukebox.getState().list).toEqual([j])
  })

  it("removes jukebox by id", async () => {
    useJukebox.setState({ list: [box("j1"), box("j2")] })
    mocked.delete.mockResolvedValue(null)
    await useJukebox.getState().delete("j1")
    expect(useJukebox.getState().list.map(j => j.id)).toEqual(["j2"])
  })

  it("updates playing flag for matching id only", async () => {
    useJukebox.setState({ list: [box("j1"), box("j2")] })
    useJukebox.getState().updatePlaying("j1", true)
    const list = useJukebox.getState().list
    expect(list[0].is_playing).toBe(true)
    expect(list[1].is_playing).toBe(false)
  })
})
