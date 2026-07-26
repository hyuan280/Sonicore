import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import { api } from "../api/client"
import { useLibrary } from "../stores/library"
import { Card, CardGrid } from "../components/ui/card"
import { Mic2, LayoutGrid, List } from "lucide-react"

export default function ArtistsPage() {
  const { activeId, libraries } = useLibrary()
  const [artists, setArtists] = useState<any[]>([])
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [layout, setLayout] = useState<"grid" | "list">("grid")
  const perPage = 30

  const load = async () => {
    if (!activeId) return
    const ids = activeId === "__all__" ? libraries.map(l => l.id) : [activeId]
    const results = await Promise.all(ids.map(id => api.data.artists(id, 1, 9999)))
    const merged = results.flatMap(r => r.items || [])
    const totalItems = results.reduce((sum, r) => sum + (r.total || 0), 0)
    const start = (page - 1) * perPage
    setArtists(merged.slice(start, start + perPage))
    setTotal(totalItems)
  }

  useEffect(() => { load() }, [activeId, page, libraries.length])

  const totalPages = Math.ceil(total / perPage)

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Artists</h1>
        <div className="flex items-center gap-3">
          <button onClick={() => setLayout(l => l === "grid" ? "list" : "grid")}
            className="p-1.5 rounded-lg bg-zinc-800 text-zinc-400 hover:text-white cursor-pointer">
            {layout === "grid" ? <List className="w-4 h-4" /> : <LayoutGrid className="w-4 h-4" />}
          </button>
          <span className="text-sm text-zinc-400">{total} artists</span>
        </div>
      </div>

      {layout === "grid" ? (
        <CardGrid>
          {artists.map(a => (
            <Link key={a.id} to={`/artists/${a.id}?lib=${a.library_id || activeId}`} className="block">
              <Card className="flex flex-col hover:bg-zinc-800/50 transition-colors">
                <div className="aspect-square rounded-full bg-zinc-800 mb-3 flex items-center justify-center">
                  <Mic2 className="w-8 h-8 text-zinc-500" />
                </div>
              <p className="font-medium text-sm text-center">{a.name}</p>
              <p className="text-xs text-zinc-500 text-center">{a.album_count} albums</p>
              </Card>
            </Link>
          ))}
        </CardGrid>
      ) : (
        <div className="space-y-1">
          {artists.map(a => (
            <Link key={a.id} to={`/artists/${a.id}?lib=${a.library_id || activeId}`}
              className="flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-zinc-800/50 transition-colors">
              <div className="w-8 h-8 rounded-full bg-zinc-800 flex items-center justify-center shrink-0">
                <Mic2 className="w-4 h-4 text-zinc-500" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm truncate">{a.name}</p>
                <p className="text-xs text-zinc-500 truncate">{a.album_count} albums</p>
              </div>
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
