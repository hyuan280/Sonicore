import { useEffect, useState, useRef, useCallback } from "react"
import { useTranslation } from "react-i18next"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { api } from "../api/client"
import { Button } from "../components/ui/button"
import { Play, Trash2, X } from "lucide-react"
import TrackTable, { type TrackRow } from "../components/TrackTable"
import { usePerPage } from "../hooks/usePerPage"

export default function HistoryPage() {
  const { t } = useTranslation()
  const player = usePlayer()
  const [records, setRecords] = useState<any[]>([])
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = usePerPage("tracks", 20)
  const [total, setTotal] = useState(0)
  const [searchQ, setSearchQ] = useState("")
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const load = useCallback(async () => {
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
    if (searchQ.trim()) params.set("q", searchQ.trim())
    const d = await fetch(`/api/user/history/list?${params}`, { headers: { Authorization: "Bearer " + localStorage.getItem("token") } }).then(r => r.json())
    const items = d.items || []
    setRecords(items)
    setTotal(d.total || 0)
    if (items.length > 0) {
      const fav = await api.user.checkFavorites(items.map((h: any) => h.track_id))
      setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
    }
  }, [page, perPage, searchQ])

  useEffect(() => { load() }, [page, perPage])

  useEffect(() => {
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => { load() }, 500)
    return () => clearTimeout(timerRef.current)
  }, [searchQ])

  const tracks: TrackRow[] = records.map(h => ({
    id: h.id,
    trackId: h.track_id,
    title: h.title || t("common.unknown"),
    duration: h.duration || 0,
    suffix: h.suffix || "mp3",
    cover_image_id: h.cover_image_id,
    artists: h.artists,
    albums: h.albums,
    versions: h.versions,
  }))

  const playAll = () => {
    const ptracks: PlayerTrack[] = records.map(h => ({
      id: h.track_id, title: h.title || t("common.unknown"), duration: h.duration || 0,
      suffix: h.suffix || "mp3", cover_image_id: h.cover_image_id,
      albums: h.albums,
    }))
    if (ptracks.length > 0) player.setQueue(ptracks, 0)
  }

  const playTrack = (h: any) => {
    const track: PlayerTrack = {
      id: h.track_id, title: h.title || t("common.unknown"), duration: h.duration || 0,
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

  const deleteItem = async (id: string) => {
    await api.user.deleteHistoryItems([id])
    setRecords(prev => prev.filter(h => h.id !== id))
  }

  const batchDelete = async () => {
    if (!confirm(t("history.removeEntries", { count: selected.size }))) return
    await api.user.deleteHistoryItems([...selected])
    setRecords(prev => prev.filter(h => !selected.has(h.id)))
    setSelected(new Set())
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
          setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n })
        }}
        extraBulkActions={
          <button onClick={batchDelete}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-red-400 cursor-pointer">
            <Trash2 className="w-4 h-4" /> {t("trackTable.delete")}
          </button>
        }
        onPlay={(i) => {
          const h = records.find(r => r.id === tracks[i]?.id)
          if (h) playTrack(h)
        }}
        currentTrackId={player.track?.id ?? null}
        extraColumnHeader={t("trackTable.playedAt")}
        extraColumn={(row) => {
          const h = records.find(r => r.id === row.id)
          if (!h?.played_at) return null
          return (
            <span className="leading-tight">
              <span className="text-zinc-500 text-xs">{new Date(h.played_at).toLocaleDateString()}</span>
              <br />
              <span className="text-zinc-500 text-[10px]">{new Date(h.played_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
            </span>
          )
        }}
        extraAction={(t) => (
          <button onClick={e => { e.stopPropagation(); deleteItem(t.id) }}
            className="text-zinc-500 hover:text-red-400 opacity-0 group-hover:opacity-100 cursor-pointer">
            <Trash2 className="w-4 h-4" />
          </button>
        )}
        emptyText={t("trackTable.noHistoryYet")}
        header={
          <div className="relative flex items-center">
            <div className="shrink-0">
              <h1 className="text-2xl font-bold">{t("nav.history")}</h1>
            </div>
            <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
              <input
                type="text"
                placeholder={t("search.searchHistory")}
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
              <Button size="sm" onClick={playAll}><Play className="w-4 h-4 mr-1" />{t("player.playAll")}</Button>
            )}
          </div>
        }
      />
    </div>
  )
}
