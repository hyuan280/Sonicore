import { useEffect, useRef, useState } from "react"
import { api } from "../api/client"
import { usePlayer } from "../stores/player"
import { Plus, ListMusic, Check, Heart, ListPlus, FileMusic } from "lucide-react"

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
  track: { id: string; title: string; duration: number; suffix?: string; file_format?: string; cover_image_id?: string; artists?: { artist_id: string; name: string; role: string }[]; albums?: { id?: string; title?: string; cover_image_id?: string }[] }
  versions?: { id: string; version: number; version_label: string; suffix: string; bit_rate: number; duration: number; library_id: string }[]
}

export function AddQueueBtn({ track, versions }: AddQueueProps) {
  const ps = usePlayer()
  const [open, setOpen] = useState(false)
  const [flipUp, setFlipUp] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const trackSuffix = track.suffix || track.file_format || "mp3"

  const hasVersions = versions && versions.length > 0

  useEffect(() => {
    if (!open) { setFlipUp(false); return }
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [open])

  const addTrack = (id: string, title: string, dur: number, suffix: string, label?: string) => {
    ps.addToQueue([{
      id, title,
      duration: dur, suffix: suffix || trackSuffix,
      cover_image_id: track.cover_image_id, artists: (track as any).artists,
      albums: (track as any).albums,
      version: (track as any).version, version_label: label || (track as any).version_label,
      versions: versions,
    }])
    setOpen(false)
  }

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (hasVersions) {
      if (!open && ref.current) {
        const btnRect = ref.current.getBoundingClientRect()
        const estH = 80 + (versions?.length || 0) * 36
        setFlipUp(btnRect.bottom + estH + 8 > window.innerHeight)
      }
      setOpen(!open)
    } else {
      addTrack(track.id, track.title, track.duration, trackSuffix)
    }
  }

  const posClass = flipUp ? "bottom-7" : "top-7"

  return (
    <div ref={ref} className="relative inline-flex">
      <button onClick={handleClick}
        className={`p-1 cursor-pointer transition-colors ${open ? "text-blue-400" : "text-zinc-500 hover:text-blue-400"}`}
        title={hasVersions ? "Select version to add" : "Add to queue"}>
        <Plus className="w-4 h-4" />
      </button>
      {open && hasVersions && (
        <div
          className={`absolute left-1/2 -translate-x-1/2 ${posClass} w-64 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-[60] py-1 max-h-72 overflow-y-auto`}
          onClick={e => e.stopPropagation()}>
          <p className="text-xs text-zinc-500 px-3 py-1.5">Select version</p>
          <div className="border-t border-zinc-700 pt-1">
            <button onClick={() => addTrack(track.id, track.title, track.duration, trackSuffix)}
              className="w-full text-left px-3 py-1.5 text-sm hover:bg-zinc-700 cursor-pointer flex items-center gap-2">
              <FileMusic className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
              <span className="text-zinc-200">{trackSuffix.toUpperCase()} · {track.title.slice(0, 15)}</span>
              <span className="text-xs text-green-500 ml-auto">current</span>
            </button>
            {versions.map(v => (
              <button key={v.id} onClick={() => addTrack(v.id, track.title, v.duration, v.suffix, v.version_label)}
                className="w-full text-left px-3 py-1.5 text-sm hover:bg-zinc-700 cursor-pointer flex items-center gap-2">
                <FileMusic className="w-3.5 h-3.5 text-blue-400 flex-shrink-0" />
                <span className="text-zinc-300 truncate">{v.version_label}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
