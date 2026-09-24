import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { usePlayer } from "../stores/player";
import { Plus, ListMusic, Check, Heart, ListPlus, FileMusic, Loader2 } from "lucide-react";
import { InlineError } from "./ui/inline-error";
import { useInlineError } from "../hooks/useInlineError";

interface Props {
  trackId: string;
  onDone?: () => void;
}

export function AddBtn({ trackId, onDone }: Props) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const { error, setError, anchorRef: ref } = useInlineError();
  const [playlists, setPlaylists] = useState<{ id: string; name: string; has: boolean }[]>([]);

  // Load first, open only on success: a failure shows the error bubble without
  // ever flashing an empty "no playlists" dropdown.
  const toggle = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (open) {
      setOpen(false);
      return;
    }
    setError(null);
    setLoading(true);
    try {
      const d = await api.user.playlists();
      const all = d.items || [];
      setPlaylists(
        all.map((p: any) => ({
          id: p.id,
          name: p.name,
          has: Array.isArray(p.track_ids) && p.track_ids.includes(trackId),
        })),
      );
      setOpen(true);
    } catch (err) {
      console.warn("[add-to-playlist] load failed", err);
      setError(t("trackTable.loadPlaylistsFailed"));
    } finally {
      setLoading(false);
    }
  };

  // Close the playlist dropdown on outside click (the error bubble is dismissed
  // independently by useInlineError).
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, ref]);

  const add = async (plId: string) => {
    setError(null);
    try {
      await api.user.addTracksToPlaylist(plId, [trackId]);
    } catch (err) {
      console.warn("[add-to-playlist] failed", err);
      setError(t("trackTable.addToPlaylistFailed"));
      setOpen(false);
      return;
    }
    setPlaylists((prev) => prev.map((p) => (p.id === plId ? { ...p, has: true } : p)));
    onDone?.();
  };

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        onClick={toggle}
        disabled={loading}
        className="p-1 text-zinc-500 hover:text-green-500 cursor-pointer disabled:cursor-default"
        title={t("trackTable.addToPlaylist")}
      >
        {loading ? (
          <Loader2 className="w-4 h-4 animate-spin text-green-500" />
        ) : (
          <ListPlus className="w-4 h-4" />
        )}
      </button>
      {error && <InlineError message={error} />}
      {open && (
        <div
          className="absolute left-1/2 -translate-x-1/2 top-7 w-52 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-[60] py-1 max-h-48 overflow-y-auto"
          onClick={(e) => e.stopPropagation()}
        >
          <p className="text-xs text-zinc-500 px-3 py-1.5">{t("trackTable.addToPlaylist")}</p>
          {playlists.length === 0 && (
            <p className="text-xs text-zinc-600 px-3 py-2">{t("trackTable.noPlaylistsYet")}</p>
          )}
          {playlists.map((p) => (
            <button
              key={p.id}
              onClick={() => !p.has && add(p.id)}
              className={`w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 cursor-pointer ${p.has ? "text-zinc-600" : "hover:bg-zinc-700 text-zinc-300"}`}
              disabled={p.has}
            >
              {p.has ? (
                <Check className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
              ) : (
                <ListMusic className="w-3.5 h-3.5 flex-shrink-0" />
              )}
              {p.name}
              {p.has && (
                <span className="text-xs text-zinc-600 ml-auto">{t("trackTable.added")}</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

interface FavProps {
  trackId: string;
  initiallyFav?: boolean;
  onToggle?: (trackId: string, nowFav: boolean) => void;
}

export function FavBtn({ trackId, initiallyFav, onToggle }: FavProps) {
  const { t } = useTranslation();
  const [fav, setFav] = useState(initiallyFav || false);
  const { error, setError, anchorRef: ref } = useInlineError();

  useEffect(() => {
    setFav(initiallyFav || false);
  }, [initiallyFav, trackId]);

  const toggle = async (e: React.MouseEvent) => {
    e.stopPropagation();
    const newFav = !fav;
    setError(null);
    try {
      if (newFav) {
        await api.user.addFavorites("track", [trackId]);
      } else {
        await api.user.removeFavorites("track", [trackId]);
      }
    } catch (err) {
      console.warn("[favorite] update failed", err);
      setError(t("trackTable.favoriteFailed"));
      return;
    }
    setFav(newFav);
    onToggle?.(trackId, newFav);
  };

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        onClick={toggle}
        className={`p-1 cursor-pointer text-zinc-500 hover:text-red-400`}
        title={fav ? t("player.removeFromFavorites") : t("player.addToFavorites")}
      >
        <Heart className={`w-4 h-4 ${fav ? "fill-current" : ""}`} />
      </button>
      {error && <InlineError message={error} />}
    </div>
  );
}

interface AddQueueProps {
  track: {
    id: string;
    title: string;
    duration: number;
    suffix?: string;
    file_format?: string;
    cover_image_id?: string;
    artists?: { artist_id: string; name: string; role: string }[];
    albums?: { id?: string; title?: string; cover_image_id?: string }[];
  };
  versions?: {
    id: string;
    version: number;
    version_label: string;
    suffix: string;
    bit_rate: number;
    duration: number;
    library_id: string;
  }[];
}

export function AddQueueBtn({ track, versions }: AddQueueProps) {
  const { t } = useTranslation();
  const ps = usePlayer();
  const [open, setOpen] = useState(false);
  const [flipUp, setFlipUp] = useState(false);
  const { error, setError, anchorRef: ref } = useInlineError();
  const trackSuffix = track.suffix || track.file_format || "mp3";

  const hasVersions = versions && versions.length > 0;

  // Close the version dropdown on outside click (the error bubble is dismissed
  // independently by useInlineError).
  useEffect(() => {
    if (!open) {
      setFlipUp(false);
      return;
    }
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, ref]);

  const addTrack = async (
    id: string,
    title: string,
    dur: number,
    suffix: string,
    label?: string,
  ) => {
    setError(null);
    // A false result means the server sync failed after the local queue was
    // already updated; this is intentionally surfaced as a failure.
    const ok = await ps.addToQueue([
      {
        id,
        title,
        duration: dur,
        suffix: suffix || trackSuffix,
        cover_image_id: track.cover_image_id,
        artists: (track as any).artists,
        albums: (track as any).albums,
        version: (track as any).version,
        version_label: label || (track as any).version_label,
        versions: versions,
      },
    ]);
    if (!ok) {
      setError(t("trackTable.queueFailed"));
      return;
    }
    setOpen(false);
  };

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (hasVersions) {
      if (!open && ref.current) {
        const btnRect = ref.current.getBoundingClientRect();
        const estH = 80 + (versions?.length || 0) * 36;
        setFlipUp(btnRect.bottom + estH + 8 > window.innerHeight);
      }
      setOpen(!open);
    } else {
      addTrack(track.id, track.title, track.duration, trackSuffix);
    }
  };

  const posClass = flipUp ? "bottom-7" : "top-7";

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        onClick={handleClick}
        className={`p-1 cursor-pointer transition-colors ${open ? "text-blue-400" : "text-zinc-500 hover:text-blue-400"}`}
        title={hasVersions ? t("player.selectVersionToAdd") : t("player.addToQueue")}
      >
        <Plus className="w-4 h-4" />
      </button>
      {error && <InlineError message={error} />}
      {open && hasVersions && (
        <div
          className={`absolute left-1/2 -translate-x-1/2 ${posClass} w-64 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-[60] py-1 max-h-72 overflow-y-auto`}
          onClick={(e) => e.stopPropagation()}
        >
          <p className="text-xs text-zinc-500 px-3 py-1.5">{t("player.selectVersion")}</p>
          <div className="border-t border-zinc-700 pt-1">
            <button
              onClick={() => addTrack(track.id, track.title, track.duration, trackSuffix)}
              className="w-full text-left px-3 py-1.5 text-sm hover:bg-zinc-700 cursor-pointer flex items-center gap-2"
            >
              <FileMusic className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
              <span className="text-zinc-200">
                {trackSuffix.toUpperCase()} · {track.title.slice(0, 15)}
              </span>
              <span className="text-xs text-green-500 ml-auto">{t("player.current")}</span>
            </button>
            {versions.map((v) => (
              <button
                key={v.id}
                onClick={() => addTrack(v.id, track.title, v.duration, v.suffix, v.version_label)}
                className="w-full text-left px-3 py-1.5 text-sm hover:bg-zinc-700 cursor-pointer flex items-center gap-2"
              >
                <FileMusic className="w-3.5 h-3.5 text-blue-400 flex-shrink-0" />
                <span className="text-zinc-300 truncate">{v.version_label}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
