import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { api } from "../api/client"
import { useLibrary } from "../stores/library"
import { Card, CardGrid } from "../components/ui/card"
import { Disc3, LayoutGrid, List, Music } from "lucide-react"
import { coverUrl } from "../lib/utils"

interface AlbumItem {
  id: string; title: string; name: string; artist: string; artistId?: string; year: number
  song_count: number; cover_image_id?: string; country?: string
}

export default function AlbumsPage() {
  const { activeId } = useLibrary()
  const [albums, setAlbums] = useState<AlbumItem[]>([])
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [layout, setLayout] = useState<"grid" | "list">("grid")
  const perPage = 30

  const load = async () => {
    if (!activeId) return
    const r = await api.data.albums(1, 9999)
    const items = r.items || []
    setTotal(items.length)
    const start = (page - 1) * perPage
    setAlbums(items.slice(start, start + perPage))
  }

  useEffect(() => { load() }, [activeId, page])

  const totalPages = Math.ceil(total / perPage)

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Albums</h1>
        <div className="flex items-center gap-3">
          <button onClick={() => setLayout(l => l === "grid" ? "list" : "grid")}
            className="p-1.5 rounded-lg bg-zinc-800 text-zinc-400 hover:text-white cursor-pointer">
            {layout === "grid" ? <List className="w-4 h-4" /> : <LayoutGrid className="w-4 h-4" />}
          </button>
          <span className="text-sm text-zinc-400">{total} albums</span>
        </div>
      </div>

      {layout === "grid" ? (
        <CardGrid>
          {albums.map(a => (
            <Link key={a.id} to={`/albums/${a.id}`} className="block">
              <Card className="hover:bg-zinc-800/50 transition-colors h-full p-0 overflow-hidden">
                <div className="aspect-square flex items-center justify-center overflow-hidden bg-zinc-800">
                  {a.cover_image_id ? (
                    <img src={coverUrl("album", a.id, 256)} alt={a.title || a.name}
                      className="w-full h-full object-cover"
                      onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                  ) : null}
                  <Disc3 className={`w-8 h-8 text-zinc-600 ${a.cover_image_id ? "hidden" : ""}`} />
                </div>
                <div className="p-3">
                <p className="font-medium text-sm truncate">{a.title || a.name}</p>
                {a.artistId ? (
                  <Link to={`/artists/${a.artistId}`} className="text-xs text-zinc-400 truncate hover:text-white transition-colors block" onClick={e => e.stopPropagation()}>{a.artist || ""}</Link>
                ) : (
                  <p className="text-xs text-zinc-400 truncate">{a.artist || ""}</p>
                )}
                <div className="flex items-center gap-2 text-xs text-zinc-500 mt-1">
                  <span>{a.year || ""}</span>
                  {a.country && <span>· {a.country}</span>}
                  {a.song_count > 0 && <span>· {a.song_count} tracks</span>}
                </div>
                </div>
              </Card>
            </Link>
          ))}
        </CardGrid>
      ) : (
        <div className="space-y-1">
          {albums.map(a => (
            <Link key={a.id} to={`/albums/${a.id}`}
              className="flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-zinc-800/50 transition-colors">
              <div className="w-8 h-8 rounded bg-zinc-800 flex items-center justify-center shrink-0 overflow-hidden">
                {a.cover_image_id ? (
                  <img src={coverUrl("album", a.id, 256)} alt={a.title || a.name}
                    className="w-full h-full object-cover"
                    onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                ) : null}
                <Disc3 className={`w-4 h-4 text-zinc-500 ${a.cover_image_id ? "hidden" : ""}`} />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm truncate">{a.title || a.name}</p>
                {a.artistId ? (
                  <Link to={`/artists/${a.artistId}`} className="text-xs text-zinc-500 truncate hover:text-white transition-colors block" onClick={e => e.stopPropagation()}>{a.artist || ""}</Link>
                ) : (
                  <p className="text-xs text-zinc-500 truncate">{a.artist || ""}</p>
                )}
              </div>
              <span className="text-xs text-zinc-500">{a.year || ""}</span>
            </Link>
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 pt-4">
          <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}
            className="px-3 py-1.5 rounded-lg text-sm bg-zinc-800 hover:bg-zinc-700 disabled:opacity-30 cursor-pointer">Prev</button>
          <span className="text-sm text-zinc-400">{page} / {totalPages}</span>
          <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}
            className="px-3 py-1.5 rounded-lg text-sm bg-zinc-800 hover:bg-zinc-700 disabled:opacity-30 cursor-pointer">Next</button>
        </div>
      )}
    </div>
  )
}
