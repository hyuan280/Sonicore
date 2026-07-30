import { useEffect, useRef, useState } from "react"
import { api } from "../api/client"
import { usePlayer } from "../stores/player"
import { Plus, ListMusic, Check, Heart, ListPlus } from "lucide-react"

interface Props {
  trackId: string
  onDone?: () => void
}

export function AddBtn({ trackId, onDone }: Props) {
  const [open, setOpen] = useState(false)
  const [playlists, setPlaylists] = useState<{ id: string; name: string; has: boolean }[]>([])
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    api.user.playlists().then(d => {
      const all = d.items || []
      Promise.all(all.map((p: any) =>
        api.user.getPlaylist(p.id).then((pl: any) => ({
          id: p.id,
          name: p.name,
          has: (pl.tracks || []).some((t: any) => t.id === trackId),
        })).catch(() => ({ id: p.id, name: p.name, has: false }))
      )).then(setPlaylists)
    })
  }, [open, trackId])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [open])

  const add = async (plId: string) => {
    await api.user.addTracksToPlaylist(plId, [trackId])
    setPlaylists(prev => prev.map(p => p.id === plId ? { ...p, has: true } : p))
    onDone?.()
  }

  return (
    <div ref={ref} className="relative inline-flex">
        <button onClick={e => { e.stopPropagation(); setOpen(!open) }}
          className="p-1 text-zinc-500 hover:text-green-500 cursor-pointer" title="Add to playlist">
          <ListPlus className="w-4 h-4" />
        </button>
      {open && (
        <div className="absolute left-1/2 -translate-x-1/2 top-7 w-52 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-[60] py-1 max-h-48 overflow-y-auto"
          onClick={e => e.stopPropagation()}>
          <p className="text-xs text-zinc-500 px-3 py-1.5">Add to playlist</p>
          {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No playlists yet</p>}
          {playlists.map(p => (
            <button key={p.id} onClick={() => !p.has && add(p.id)}
              className={`w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 cursor-pointer ${p.has ? "text-zinc-600" : "hover:bg-zinc-700 text-zinc-300"}`}
              disabled={p.has}>
              {p.has ? <Check className="w-3.5 h-3.5 text-green-500 flex-shrink-0" /> : <ListMusic className="w-3.5 h-3.5 flex-shrink-0" />}
              {p.name}
              {p.has && <span className="text-xs text-zinc-600 ml-auto">added</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

interface FavProps {
  trackId: string
  initiallyFav?: boolean
  onToggle?: (trackId: string, nowFav: boolean) => void
}

export function FavBtn({ trackId, initiallyFav, onToggle }: FavProps) {
  const [fav, setFav] = useState(initiallyFav || false)
  useEffect(() => { setFav(initiallyFav || false) }, [initiallyFav, trackId])

  const toggle = async (e: React.MouseEvent) => {
    e.stopPropagation()
    const newFav = !fav
    if (newFav) {
      await api.user.addFavorites("track", [trackId]).catch(() => {})
    } else {
      await api.user.removeFavorites("track", [trackId]).catch(() => {})
    }
    setFav(newFav)
    onToggle?.(trackId, newFav)
  }

  return (
    <button onClick={toggle}
      className={`p-1 cursor-pointer text-zinc-500 hover:text-red-400`}
      title={fav ? "Remove from favorites" : "Add to favorites"}>
      <Heart className={`w-4 h-4 ${fav ? "fill-current" : ""}`} />
    </button>
  )
}

interface AddQueueProps {
  track: { id: string; title: string; duration: number; suffix?: string; cover_image_id?: string; artists?: { artist_id: string; name: string; role: string }[]; albums?: { id?: string; title?: string }[] }
}

export function AddQueueBtn({ track }: AddQueueProps) {
  const ps = usePlayer()
  return (
    <button onClick={e => {
      e.stopPropagation()
        ps.addToQueue([{
          id: track.id, title: track.title,
          duration: track.duration, suffix: track.suffix || "mp3",
          cover_image_id: track.cover_image_id, artists: (track as any).artists,
          albums: (track as any).albums,
        }])
    }}
      className="p-1 text-zinc-500 hover:text-blue-400 cursor-pointer" title="Add to queue">
      <Plus className="w-4 h-4" />
    </button>
  )
}
