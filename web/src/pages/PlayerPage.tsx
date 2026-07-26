import { usePlayer } from "../stores/player"
import { Button } from "../components/ui/button"
import { SkipForward, Music, Play } from "lucide-react"
import { formatDuration } from "../lib/utils"

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
        {ps.queue.map((t, i) => (
          <div key={t.id + i}
            className={`flex items-center px-4 py-2 rounded-lg cursor-pointer group ${i === ps.queueIdx ? "bg-green-600/10" : "hover:bg-zinc-800/50"}`}
            onClick={() => ps.playIndex(i)}>
            <span className={`w-8 text-sm ${i === ps.queueIdx ? "text-green-500" : "text-zinc-500"} group-hover:hidden`}>{i + 1}</span>
            <Play className={`w-4 h-4 hidden group-hover:block mr-4 ${i === ps.queueIdx ? "text-green-500" : "text-green-500"}`} />
            <span className={`flex-1 text-sm truncate ${i === ps.queueIdx ? "text-green-500" : ""}`}>{t.title}</span>
            <span className="text-xs text-zinc-500 mr-2 truncate max-w-40">{t.artist}</span>
            <span className="w-16 text-right text-sm text-zinc-400">{formatDuration(t.duration)}</span>
          </div>
        ))}
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
