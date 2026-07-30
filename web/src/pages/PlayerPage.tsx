import { usePlayer } from "../stores/player"
import { Button } from "../components/ui/button"
import { SkipForward, Music, Play } from "lucide-react"
import { Link } from "react-router-dom"
import { formatDuration, performerNames, coverUrl } from "../lib/utils"
import ArtistLink from "../components/ArtistLink"

export default function PlayerPage() {
  const ps = usePlayer()

  return (
    <div className="p-6 space-y-6 pb-24">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Player Queue</h1>
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" onClick={ps.clearQueue}>Clear</Button>
        </div>
      </div>

      <div className="space-y-1">
        <div className="flex items-center text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          <span className="w-8">#</span>
          <span className="flex-1">Title</span>
          <span className="w-16 text-right">Duration</span>
        </div>
        {ps.queue.map((t, i) => {
          return (
            <div key={t.id + i}
              className={`flex items-center px-4 py-2 rounded-lg cursor-pointer group ${i === ps.queueIdx ? "bg-green-600/10" : "hover:bg-zinc-800/50"}`}
              onClick={() => ps.playIndex(i)}>
              <span className={`w-8 text-sm ${i === ps.queueIdx ? "text-green-500" : "text-zinc-500"} group-hover:hidden`}>{i + 1}</span>
              <Play className={`w-4 h-4 hidden group-hover:block mr-4 ${i === ps.queueIdx ? "text-green-500" : "text-green-500"}`} />
              <div className="w-10 h-10 rounded bg-zinc-800 flex-shrink-0 overflow-hidden mr-3">
                {t.cover_image_id ? (
                  <img src={coverUrl("track", t.id, 64)} alt="" className="w-full h-full object-cover" />
                ) : (
                  <div className="w-full h-full flex items-center justify-center">
                    <Music className="w-4 h-4 text-zinc-600" />
                  </div>
                )}
              </div>
              <div className="flex-1 min-w-0">
                <span className={`text-sm truncate block ${i === ps.queueIdx ? "text-green-500" : ""}`}>{t.title}</span>
                <span className="text-xs text-zinc-500 truncate block">
                  <ArtistLink artists={t.artists} />{t.albums?.[0]?.title ? ` — ${t.albums[0].id ? <Link to={`/albums/${t.albums[0].id}`} className="hover:text-white transition-colors" onClick={e => e.stopPropagation()}>{t.albums[0].title}</Link> : t.albums[0].title}` : ""}
                </span>
              </div>
              <span className="w-16 text-right text-sm text-zinc-400">{formatDuration(t.duration)}</span>
            </div>
          )
        })}
        {ps.queue.length === 0 && <p className="text-zinc-500 text-center py-12">Queue is empty</p>}
      </div>

      <div className="flex gap-2 items-center">
        <span className="text-sm text-zinc-400">Loop:</span>
          {(["normal", "all", "one", "shuffle"] as const).map(m => (
            <Button key={m} variant={ps.mode === m ? "primary" : "ghost"} size="sm" onClick={() => ps.cycleMode()}>
              {m === "normal" ? "Normal" : m === "all" ? "All" : m === "one" ? "One" : "Shuffle"}
          </Button>
        ))}
      </div>
    </div>
  )
}
