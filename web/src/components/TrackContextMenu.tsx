import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, ListPlus, Heart, Download, Check, ListMusic } from "lucide-react";
import { api } from "../api/client";
import { usePlayer } from "../stores/player";
import { usePlaylists, type Playlist } from "../stores/playlists";
import { isAdmin } from "../stores/auth";
import { cn, streamDownloadUrl } from "../lib/utils";
import { ContextMenu, ContextMenuItem } from "./ui/context-menu";
import type { TrackRow } from "./TrackTable";

interface Props {
  track: TrackRow;
  x: number;
  y: number;
  favoriteIds: Set<string>;
  onFavoriteToggle?: (id: string, nowFav: boolean) => void;
  onPlaylistChange?: () => void;
  playlistFilter?: (pl: Playlist) => boolean;
  onClose: () => void;
}

export default function TrackContextMenu({
  track,
  x,
  y,
  favoriteIds,
  onFavoriteToggle,
  onPlaylistChange,
  playlistFilter,
  onClose,
}: Props) {
  const { t } = useTranslation();
  const player = usePlayer();
  const playlists = usePlaylists((s) => s.list);
  const loadPlaylists = usePlaylists((s) => s.load);
  const playlistsLoading = usePlaylists((s) => s.loading);
  const id = track.trackId || track.id;
  const [fav, setFav] = useState(favoriteIds.has(id));
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    setFav(favoriteIds.has(id));
  }, [favoriteIds, id]);

  useEffect(() => {
    // Force a refresh: the cached track_ids may be stale because other entry
    // points (e.g. the inline AddBtn) also add tracks without updating the
    // store, which would otherwise let an already-added playlist be re-added.
    loadPlaylists(true).then((ok) => {
      setLoadFailed(!ok);
      if (!ok) setError(t("trackTable.loadPlaylistsFailed"));
    });
  }, [loadPlaylists, t]);

  const items = (playlistFilter ? playlists.filter(playlistFilter) : playlists).map((p) => ({
    id: p.id,
    name: p.name,
    has: Array.isArray(p.track_ids) && p.track_ids.includes(id),
  }));

  // Returns false on failure so MenuItem keeps the menu open to show the error.
  // A false result means the server sync failed; the local queue was already
  // updated. This is intentionally reported as a failure rather than a milder
  // "synced later" distinction.
  const addToQueue = async (): Promise<boolean> => {
    setError("");
    try {
      const ok = await player.addToQueue([
        {
          id,
          title: track.title,
          duration: track.duration,
          suffix: track.suffix || "mp3",
          cover_image_id: track.cover_image_id,
          artists: track.artists,
          albums: track.albums,
          versions: track.versions,
        },
      ]);
      if (!ok) {
        setError(t("trackTable.queueFailed"));
        return false;
      }
    } catch (err) {
      console.warn("[track-menu] add to queue failed", err);
      setError(t("trackTable.queueFailed"));
      return false;
    }
    return true;
  };

  // Returns false on failure so MenuItem keeps the menu open to show the error.
  const toggleFavorite = async (): Promise<boolean> => {
    const next = !fav;
    setError("");
    try {
      if (next) {
        await api.user.addFavorites("track", [id]);
      } else {
        await api.user.removeFavorites("track", [id]);
      }
    } catch (err) {
      console.warn("[track-menu] favorite update failed", err);
      setError(t("trackTable.favoriteFailed"));
      return false;
    }
    setFav(next);
    onFavoriteToggle?.(id, next);
    return true;
  };

  const addToPlaylist = async (plId: string): Promise<boolean> => {
    setError("");
    try {
      await api.user.addTracksToPlaylist(plId, [id]);
    } catch (err) {
      console.warn("[track-menu] add to playlist failed", err);
      setError(t("trackTable.addToPlaylistFailed"));
      return false;
    }
    onPlaylistChange?.();
    return true;
  };

  // A plain <a download> cannot report HTTP failures, so probe the download
  // endpoint with a one-byte Range request first; only start the transfer when
  // the file is actually reachable and permitted.
  const download = async (): Promise<boolean> => {
    setError("");
    const url = streamDownloadUrl(id);
    try {
      const res = await fetch(url, { headers: { Range: "bytes=0-0" } });
      await res.body?.cancel();
      if (!res.ok) {
        console.warn("[track-menu] download failed", res.status);
        setError(t("trackTable.downloadFailed"));
        return false;
      }
    } catch (err) {
      console.warn("[track-menu] download failed", err);
      setError(t("trackTable.downloadFailed"));
      return false;
    }
    const a = document.createElement("a");
    a.href = url;
    // Empty value: let the server's Content-Disposition filename decide the
    // saved name (the real original file name with the correct extension).
    a.download = "";
    document.body.appendChild(a);
    a.click();
    a.remove();
    return true;
  };

  // Clear a previous failure only when the pointer moves onto another item.
  // The error is shown outside the panel flow (see ContextMenu), so it never
  // shifts the items under a stationary cursor, which would otherwise fire a
  // spurious hover and wipe the message before it can be read.
  const clearError = () => setError("");

  return (
    <ContextMenu x={x} y={y} onClose={onClose} error={error || undefined}>
      <ContextMenuItem
        icon={<Plus className="w-4 h-4" />}
        onClick={addToQueue}
        onHover={clearError}
      >
        {t("player.addToQueue")}
      </ContextMenuItem>
      <ContextMenuItem
        icon={<ListPlus className="w-4 h-4" />}
        onHover={clearError}
        submenu={
          loadFailed ? (
            <p className="px-3 py-2 text-xs text-red-400">{t("trackTable.loadPlaylistsFailed")}</p>
          ) : playlistsLoading ? (
            <p className="px-3 py-2 text-xs text-zinc-500">{t("common.loading")}</p>
          ) : items.length === 0 ? (
            <p className="px-3 py-2 text-xs text-zinc-500">{t("trackTable.noPlaylistsYet")}</p>
          ) : (
            items.map((p) => (
              <button
                key={p.id}
                type="button"
                role="menuitem"
                disabled={p.has}
                onClick={async (e) => {
                  e.stopPropagation();
                  if (p.has) return;
                  if (await addToPlaylist(p.id)) onClose();
                }}
                className={cn(
                  "w-full text-left px-3 py-2 text-sm flex items-center gap-2",
                  p.has
                    ? "text-zinc-600 cursor-default"
                    : "cursor-pointer text-zinc-300 hover:text-white hover:bg-zinc-700/60",
                )}
              >
                {p.has ? (
                  <Check className="w-3.5 h-3.5 text-green-500 shrink-0" />
                ) : (
                  <ListMusic className="w-3.5 h-3.5 shrink-0" />
                )}
                <span className="flex-1 truncate">{p.name}</span>
                {p.has && (
                  <span className="text-xs text-zinc-600 ml-auto">{t("trackTable.added")}</span>
                )}
              </button>
            ))
          )
        }
      >
        {t("trackTable.addToPlaylist")}
      </ContextMenuItem>
      <ContextMenuItem
        icon={<Heart className={cn("w-4 h-4", fav && "fill-current text-red-400")} />}
        onClick={toggleFavorite}
        onHover={clearError}
      >
        {fav ? t("trackTable.unfavorite") : t("trackTable.favorite")}
      </ContextMenuItem>
      {isAdmin() && (
        <ContextMenuItem
          icon={<Download className="w-4 h-4" />}
          onClick={download}
          onHover={clearError}
        >
          {t("trackTable.download")}
        </ContextMenuItem>
      )}
    </ContextMenu>
  );
}
