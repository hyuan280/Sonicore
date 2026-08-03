import { useEffect, useState, useMemo, useRef, useCallback } from "react"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { Button } from "../components/ui/button"
import { Play, X } from "lucide-react"
import TrackTable, { type TrackRow } from "../components/TrackTable"
import { usePerPage } from "../hooks/usePerPage"

export default function FavoritesPage() {
  const player = usePlayer()
  const [records, setRecords] = useState<any[]>([])
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = usePerPage("tracks", 20)
  const [total, setTotal] = useState(0)
  const [searchQ, setSearchQ] = useState("")
  const [removed, setRemoved] = useState<Set<string>>(new Set())
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const load = useCallback(async () => {
    const params = new URLSearchParams({ type: "track", page: String(page), per_page: String(perPage) })
    if (searchQ.trim()) params.set("q", searchQ.trim())
    const d = await fetch(`/api/user/favorites/list?${params}`, { headers: { Authorization: "Bearer " + localStorage.getItem("token") } }).then(r => r.json())
    setRecords(d.items || [])
    setTotal(d.total || 0)
  }, [page, perPage, searchQ])

  useEffect(() => { load() }, [page, perPage])

  useEffect(() => {
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => { load() }, 500)
    return () => clearTimeout(timerRef.current)
  }, [searchQ])

  const tracks: TrackRow[] = records.map(h => ({
    id: h.item_id, title: h.title || "Unknown", duration: h.duration || 0,
    suffix: h.suffix || "mp3", cover_image_id: h.cover_image_id,
    artists: h.artists, albums: h.albums, versions: h.versions,
  }))

  const favoriteIds = useMemo(() =>
    new Set(records.map(h => h.item_id).filter(id => !removed.has(id))),
  [records, removed])

  const playAll = () => {
    const ptracks: PlayerTrack[] = records.map(h => ({
      id: h.item_id, title: h.title || "Unknown", duration: h.duration || 0,
      suffix: h.suffix || "mp3", cover_image_id: h.cover_image_id,
      artists: h.artists, albums: h.albums,
    }))
    if (ptracks.length > 0) player.setQueue(ptracks, 0)
  }

  const playTrack = (h: any) => {
    const track: PlayerTrack = {
      id: h.item_id, title: h.title || "Unknown", duration: h.duration || 0,
      suffix: h.suffix || "mp3", cover_image_id: h.cover_image_id,
      artists: h.artists, albums: h.albums,
    }
    const existingIdx = player.queue.findIndex(t => t.id === track.id)
    if (existingIdx >= 0) { player.playIndex(existingIdx); return }
    const newIdx = player.queue.length
    player.addToQueue([track])
    player.playIndex(newIdx)
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }

  return (
    <div>
      <TrackTable
        tracks={tracks}
        page={page}
        perPage={perPage}
        total={total}
        onPageChange={setPage}
        onPerPageChange={(val) => { setPerPage(val); setPage(1) }}
        multi={multi}
        selected={selected}
        onMultiToggle={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
        onToggleSelect={toggleSelect}
        favoriteIds={favoriteIds}
        onFavoriteToggle={(id, nowFav) => {
          setRemoved(prev => { const n = new Set(prev); nowFav ? n.delete(id) : n.add(id); return n })
        }}
        onPlay={(i) => {
          const h = records.find(r => r.item_id === tracks[i]?.id)
          if (h) playTrack(h)
        }}
        currentTrackId={player.track?.id ?? null}
        header={
          <div className="relative flex items-center">
            <div className="shrink-0">
              <h1 className="text-2xl font-bold">Favorites</h1>
            </div>
            <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
              <input
                type="text"
                placeholder="Search favorites..."
                value={searchQ}
                onChange={e => { setSearchQ(e.target.value); setPage(1) }}
                className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
              />
              {searchQ && (
                <button onClick={() => { setSearchQ(""); setPage(1) }}
                  className="absolute right-5 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer">
                  <X className="w-4 h-4" />
                </button>
              )}
            </div>
            <div className="flex-1" />
            {records.length > 0 && (
              <Button size="sm" onClick={playAll}><Play className="w-4 h-4 mr-1" />Play All</Button>
            )}
          </div>
        }
        emptyText="No favorites yet"
      />
    </div>
  )
}
