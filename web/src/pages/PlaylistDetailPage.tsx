import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useParams, useNavigate } from "react-router-dom"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { usePlaylists } from "../stores/playlists"
import { Button } from "../components/ui/button"
import { Trash2, ListMusic, X } from "lucide-react"
import TrackTable, { type TrackRow } from "../components/TrackTable"
import { usePerPage } from "../hooks/usePerPage"

export default function PlaylistDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams()
  const navigate = useNavigate()
  const player = usePlayer()
  const { remove: removePlaylist } = usePlaylists()
  const [playlist, setPlaylist] = useState<any>(null)
  const [perPage, setPerPage] = usePerPage("tracks", 20)
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [delConfirm, setDelConfirm] = useState(false)
  const [favs, setFavs] = useState<Set<string>>(new Set())
  const [searchQ, setSearchQ] = useState("")

  const load = () => {
    if (!id) return
    api.user.getPlaylist(id, true).then(p => {
      setPlaylist(p)
      const ids = (p.tracks || []).map((t: any) => t.id)
      if (ids.length > 0) {
        api.user.checkFavorites(ids).then(d => {
          setFavs(new Set(Object.keys(d.favorites || {})))
        }).catch(() => {})
      }
    })
  }

  useEffect(() => { load() }, [id])

  const handleDelete = async () => {
    if (!id) return
    await removePlaylist(id)
    navigate("/playlists")
  }

  const removeTrack = async (trackId: string) => {
    if (!id) return
    await api.user.removeTracksFromPlaylist(id, [trackId])
    load()
  }

  const toggleSelect = (tid: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(tid)) n.delete(tid); else n.add(tid); return n })
  }
  const batchRemove = async () => {
    if (!id) return
    if (!confirm(t("playlist.removeTracks", { count: selected.size }))) return
    await api.user.removeTracksFromPlaylist(id, [...selected])
    setSelected(new Set())
    load()
  }

  if (!playlist) return <div className="p-6 text-zinc-500">{t("common.loading")}</div>
  const rawTracks: any[] = playlist.tracks || []
  const filteredRaw = searchQ.trim()
    ? rawTracks.filter((t: any) => t.title.toLowerCase().includes(searchQ.trim().toLowerCase()))
    : rawTracks
  const tracks: TrackRow[] = filteredRaw.map((t: any) => ({
    id: t.id, title: t.title, duration: t.duration,
    suffix: t.file_format || t.suffix || "mp3", cover_image_id: t.cover_image_id,
    artists: t.artists, albums: t.albums,
    versions: t.versions,
  }))

  return (
    <div>
      <TrackTable
        tracks={tracks}
        perPage={perPage}
        onPerPageChange={setPerPage}
        header={
          <div className="relative flex items-center">
            <div className="flex items-center gap-3 shrink-0">
              <ListMusic className={`w-8 h-8 ${id && player.currentPlaylistId === id ? "text-green-500" : "text-zinc-500"}`} />
              <div>
                <h1 className="text-xl font-bold">{playlist.name}</h1>
              </div>
              <span className="text-xs text-zinc-500 self-end pb-0.5">{filteredRaw.length} tracks</span>
            </div>
            <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
              <input
                type="text"
                placeholder={t("search.searchPlaylist")}
                value={searchQ}
                onChange={e => setSearchQ(e.target.value)}
                className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
              />
              {searchQ && (
                <button onClick={() => setSearchQ("")}
                  className="absolute right-5 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer">
                  <X className="w-4 h-4" />
                </button>
              )}
            </div>
            <div className="flex-1" />
            <div className="flex gap-1">
              {delConfirm ? (
                <div className="flex gap-1 items-center">
                  <button onClick={() => { handleDelete(); setDelConfirm(false) }}
                    className="text-xs px-2 py-1 rounded bg-red-600/20 text-red-400 hover:bg-red-600/30 cursor-pointer">{t("common.delete")}</button>
                  <button onClick={() => setDelConfirm(false)}
                    className="text-xs px-2 py-1 rounded text-zinc-500 hover:text-white cursor-pointer">{t("common.no")}</button>
                </div>
              ) : (
                <Button variant="ghost" size="sm" onClick={() => setDelConfirm(true)}>
                  <Trash2 className="w-4 h-4" />
                </Button>
              )}
            </div>
          </div>
        }
        multi={multi}
        selected={selected}
        onMultiToggle={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
        onToggleSelect={toggleSelect}
        favoriteIds={favs}
        onFavoriteToggle={(id, nowFav) => {
          setFavs(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n })
        }}
        playlistFilter={(pl: any) => pl.id !== id}
        extraBulkActions={
          <button onClick={batchRemove}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
            <Trash2 className="w-4 h-4" /> {t("trackTable.remove")}
          </button>
        }
        onPlay={(i) => {
          const playerTracks: PlayerTrack[] = (playlist?.tracks || []).map((t: any) => ({
            id: t.id, title: t.title, duration: t.duration,
            suffix: t.file_format || t.suffix || "mp3",
            cover_image_id: t.cover_image_id, artists: t.artists, albums: t.albums,
            version: t.version, version_label: t.version_label,
          }))
          player.setQueue(playerTracks, i, id)
        }}
        currentTrackId={player.track?.id ?? null}
        extraAction={(t) => (
          <button onClick={e => { e.stopPropagation(); removeTrack(t.id) }}
            className="text-zinc-500 hover:text-red-400 opacity-0 group-hover:opacity-100 cursor-pointer">
            <Trash2 className="w-4 h-4" />
          </button>
        )}
        emptyText={t("trackTable.noTracksPlaylist")}
      />
    </div>
  )
}
