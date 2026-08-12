import { describe, expect, it, beforeEach, vi } from "vitest"

vi.mock("../api/client", () => ({
  api: {
    user: {
      playlists: vi.fn(),
      createPlaylist: vi.fn(),
      deletePlaylist: vi.fn(),
    },
  },
}))

import { api } from "../api/client"
import { usePlaylists } from "./playlists"

const mocked = vi.mocked(api.user)

describe("usePlaylists", () => {
  beforeEach(() => {
    usePlaylists.setState({ list: [], loading: false, loaded: false })
    vi.clearAllMocks()
  })

  it("loads playlist items once", async () => {
    mocked.playlists.mockResolvedValue({ items: [{ id: "p1", name: "Favs" }] })
    await usePlaylists.getState().load()
    expect(usePlaylists.getState().list).toEqual([{ id: "p1", name: "Favs" }])
    expect(usePlaylists.getState().loaded).toBe(true)
  })

  it("skips reload when already loaded unless forced", async () => {
    mocked.playlists.mockResolvedValue({ items: [] })
    await usePlaylists.getState().load()
    mocked.playlists.mockResolvedValue({ items: [{ id: "p2" }] })

    await usePlaylists.getState().load()
    expect(usePlaylists.getState().list).toEqual([])

    await usePlaylists.getState().load(true)
    expect(usePlaylists.getState().list).toEqual([{ id: "p2" }])
  })

  it("keeps stale list and clears loading on failure", async () => {
    usePlaylists.setState({ list: [{ id: "old" }], loaded: true })
    mocked.playlists.mockRejectedValue(new Error("boom"))
    await usePlaylists.getState().load(true)
    expect(usePlaylists.getState().list).toEqual([{ id: "old" }])
    expect(usePlaylists.getState().loading).toBe(false)
  })

  it("appends created playlist to list", async () => {
    usePlaylists.setState({ list: [] })
    mocked.createPlaylist.mockResolvedValue({ id: "new1" })
    const res = await usePlaylists.getState().create("My List")
    expect(res).toEqual({ id: "new1" })
    const list = usePlaylists.getState().list
    expect(list).toHaveLength(1)
    expect(list[0]).toMatchObject({ id: "new1", name: "My List" })
  })

  it("removes playlist by id", async () => {
    usePlaylists.setState({ list: [{ id: "p1" }, { id: "p2" }] })
    mocked.deletePlaylist.mockResolvedValue(null)
    await usePlaylists.getState().remove("p1")
    expect(usePlaylists.getState().list.map(p => p.id)).toEqual(["p2"])
  })
})
