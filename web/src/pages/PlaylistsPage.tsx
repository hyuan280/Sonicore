import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { usePlaylists } from "../stores/playlists";
import { usePlayer } from "../stores/player";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { ListMusic, Plus, ChevronRight, Trash2, X } from "lucide-react";

export default function PlaylistsPage() {
  const { t } = useTranslation();
  const { list: playlists, load, create: createPlaylist, remove: removePlaylist } = usePlaylists();
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [delId, setDelId] = useState<string | null>(null);
  const [searchQ, setSearchQ] = useState("");
  const navigate = useNavigate();
  const currentPlaylistId = usePlayer((s) => s.currentPlaylistId);

  useEffect(() => {
    load();
  }, [load]);

  const create = async () => {
    if (!name.trim()) return;
    await createPlaylist(name.trim());
    setName("");
    setShowCreate(false);
  };

  const del = async (id: string) => {
    await removePlaylist(id);
    setDelId(null);
  };

  const filtered = searchQ.trim()
    ? playlists.filter((p: any) => p.name.toLowerCase().includes(searchQ.trim().toLowerCase()))
    : playlists;

  return (
    <div className="p-6 space-y-6">
      <div className="relative flex items-center">
        <h1 className="text-2xl font-bold shrink-0">{t("nav.playlists")}</h1>
        <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
          <input
            type="text"
            placeholder={t("search.searchPlaylists")}
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
          />
          {searchQ && (
            <button
              onClick={() => setSearchQ("")}
              className="absolute right-5 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer"
            >
              <X className="w-4 h-4" />
            </button>
          )}
        </div>
        <div className="flex-1" />
        <Button variant="primary" size="sm" onClick={() => setShowCreate(true)}>
          <Plus className="w-4 h-4 mr-1" /> {t("playlist.newPlaylist")}
        </Button>
      </div>

      <div className="space-y-2">
        {filtered.map((p) => (
          <div
            key={p.id}
            className="flex items-center gap-3 p-4 border border-zinc-800 rounded-xl bg-zinc-900/50 hover:bg-zinc-800/30 transition-colors group"
          >
            <ListMusic
              className={`w-10 h-10 shrink-0 ${currentPlaylistId === p.id ? "text-green-500" : "text-zinc-600"}`}
            />
            <div
              className="flex-1 min-w-0 cursor-pointer"
              onClick={() => navigate(`/playlists/${p.id}`)}
            >
              <div className="font-medium truncate">{p.name}</div>
              <div className="text-xs text-zinc-500">
                {Array.isArray(p.track_ids) ? p.track_ids.length : 0} tracks
              </div>
            </div>
            <div className="flex items-center gap-2">
              {delId === p.id ? (
                <>
                  <button
                    onClick={() => del(p.id)}
                    className="text-xs px-2 py-1 rounded bg-red-600/20 text-red-400 hover:bg-red-600/30 cursor-pointer"
                  >
                    {t("playlist.delete")}
                  </button>
                  <button
                    onClick={() => setDelId(null)}
                    className="text-xs px-2 py-1 rounded text-zinc-500 hover:text-white cursor-pointer"
                  >
                    {t("playlist.no")}
                  </button>
                </>
              ) : (
                <button
                  onClick={() => setDelId(p.id)}
                  className="opacity-0 group-hover:opacity-100 p-1.5 rounded text-zinc-500 hover:text-red-400 hover:bg-zinc-800 transition-all cursor-pointer"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              )}
              <ChevronRight className="w-4 h-4 text-zinc-600" />
            </div>
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="text-center py-16 space-y-4">
            <ListMusic className="w-12 h-12 text-zinc-600 mx-auto" />
            <p className="text-zinc-500">
              {playlists.length === 0
                ? t("playlist.noPlaylistsYet")
                : t("playlist.noPlaylistsMatch")}
            </p>
            <p className="text-sm text-zinc-600">
              {playlists.length === 0 ? t("playlist.getStarted") : ""}
            </p>
          </div>
        )}
      </div>

      {showCreate && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
          onClick={() => setShowCreate(false)}
        >
          <div
            className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-lg font-bold mb-4">{t("playlist.createPlaylist")}</h2>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("playlist.playlistName")}
              autoFocus
              onKeyDown={(e) => e.key === "Enter" && create()}
            />
            <div className="flex justify-end gap-2 mt-4">
              <Button variant="ghost" onClick={() => setShowCreate(false)}>
                {t("playlist.cancel")}
              </Button>
              <Button variant="primary" onClick={create} disabled={!name.trim()}>
                {t("playlist.create")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
