import { useEffect, useState, useRef, useCallback } from "react"
import { Link } from "react-router-dom"
import { api } from "../api/client"
import { Card, CardGrid } from "../components/ui/card"
import { Disc3, LayoutGrid, List, Music, X, ChevronLeft, ChevronRight, ChevronDown } from "lucide-react"
import { coverUrl } from "../lib/utils"
import { usePerPage } from "../hooks/usePerPage"

interface AlbumItem {
  id: string; title: string; name: string; artist: string; artistId?: string; year: number
  song_count: number; cover_image_id?: string; country?: string
}

export default function AlbumsPage() {
  const [albums, setAlbums] = useState<AlbumItem[]>([])
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = usePerPage("albums", 10)
  const [total, setTotal] = useState(0)
  const [layout, setLayout] = useState<"grid" | "list">("grid")
  const [searchQ, setSearchQ] = useState("")
  const [pageEditing, setPageEditing] = useState(false)
  const [perPageOpen, setPerPageOpen] = useState(false)
  const [editValue, setEditValue] = useState("")
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const commitPage = (val: string) => {
    const v = parseInt(val)
    if (v >= 1 && v <= totalPages) setPage(v)
    setPageEditing(false)
  }
  const startEdit = () => { setEditValue(""); setPageEditing(true) }

  const load = useCallback(async () => {
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
    if (searchQ.trim()) params.set("q", searchQ.trim())
    const r = await fetch(`/api/data/albums?${params}`, { headers: { Authorization: "Bearer " + localStorage.getItem("token") } }).then(r => r.json())
    setAlbums(r.items || [])
    setTotal(r.total || 0)
  }, [page, perPage, searchQ])

  useEffect(() => { load() }, [page, perPage])

  useEffect(() => {
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => { load() }, 500)
    return () => clearTimeout(timerRef.current)
  }, [searchQ])

  const totalPages = Math.ceil(total / perPage)

  return (
    <>
      <div className="sticky top-0 z-10 bg-black px-6 pt-6 pb-4 space-y-2">
        <div className="relative flex items-center">
          <h1 className="text-2xl font-bold shrink-0">Albums</h1>
          <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
            <input
              type="text"
              placeholder="Search albums..."
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
          <div className="flex items-center gap-3">
            <button onClick={() => setLayout(l => l === "grid" ? "list" : "grid")}
              className="p-1.5 rounded-lg bg-zinc-800 text-zinc-400 hover:text-white cursor-pointer">
              {layout === "grid" ? <List className="w-4 h-4" /> : <LayoutGrid className="w-4 h-4" />}
            </button>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2">
          <span className="text-sm text-zinc-400">{total} albums</span>
          <div className="flex items-center bg-zinc-800 rounded-lg">
            <div className="relative">
              <button onClick={() => setPerPageOpen(!perPageOpen)}
                className="px-3 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-l-lg cursor-pointer transition-colors">
                {perPage} <ChevronDown className="w-4 h-4 inline-block -m-0.5" />
              </button>
              {perPageOpen && (
                <div className="absolute top-full left-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1"
                  onClick={() => setPerPageOpen(false)}>
                  {[10, 20, 50].map(n => (
                    <button key={n} onClick={() => { setPerPage(n); setPage(1); setPerPageOpen(false) }}
                      className={`w-full text-left px-3 py-1.5 text-sm cursor-pointer ${perPage === n ? "text-white" : "text-zinc-400 hover:text-white"}`}>{n}</button>
                  ))}
                </div>
              )}
            </div>
            <span className="w-px h-4 bg-zinc-700" />
            <div className="w-24 flex items-center justify-center relative shrink-0">
              {pageEditing ? (
                <>
                  <input
                    type="text"
                    inputMode="numeric"
                    value={editValue}
                    onChange={e => setEditValue(e.target.value)}
                    onBlur={() => commitPage(editValue)}
                    onKeyDown={e => { if (e.key === "Enter") commitPage(editValue) }}
                    autoFocus
                    placeholder={`/ ${totalPages || 1}`}
                    className="w-full text-center py-2 text-sm bg-transparent text-zinc-400 border-none outline-none"
                  />
                  {totalPages > 1 && (
                    <div className="absolute left-0 top-full mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 max-h-48 overflow-y-auto min-w-[3rem]">
                      {Array.from({ length: totalPages }, (_, i) => i + 1).map(n => (
                        <button key={n} onMouseDown={() => { setPage(n); setPageEditing(false) }}
                          className={`w-full text-center px-3 py-1.5 text-sm cursor-pointer ${n === page ? "text-white" : "text-zinc-400 hover:text-white"}`}>
                          {n}
                        </button>
                      ))}
                    </div>
                  )}
                </>
              ) : totalPages > 1 ? (
                <span onClick={startEdit}
                  className="text-sm text-zinc-400 cursor-pointer hover:text-white hover:bg-zinc-700 w-full text-center py-2 transition-colors">
                  {page} / {totalPages}
                </span>
              ) : (
                <span className="text-sm text-zinc-400 w-full text-center py-2">
                  1 / 1
                </span>
              )}
            </div>
            <span className="w-px h-4 bg-zinc-700" />
            <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}
              className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]">
              <ChevronLeft className="w-4 h-4" />Prev
            </button>
            <span className="w-px h-4 bg-zinc-700" />
            <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}
              className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-r-lg disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]">
              Next<ChevronRight className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>

      <div className="px-6 pb-24 space-y-6">
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
        {albums.length === 0 && <p className="text-zinc-500 text-center py-12">No albums found</p>}
      </div>
    </>
  )
}
