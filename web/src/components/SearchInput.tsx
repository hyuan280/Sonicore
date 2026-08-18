import { useState, useRef, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { Music, Disc3, Mic2 } from "lucide-react";

interface SearchResult {
  tracks: {
    id: string;
    title: string;
    artists?: any[];
    albums?: any[];
    duration: number;
    suffix?: string;
  }[];
  albums: { id: string; title: string; year?: number; cover_image_id?: string }[];
  artists: { id: string; name: string; cover_image_id?: string }[];
}

interface SearchInputProps {
  placeholder?: string;
  className?: string;
  showTracks?: boolean;
  showAlbums?: boolean;
  showArtists?: boolean;
  minLen?: number;
  onSelectTrack?: (track: {
    id: string;
    title: string;
    albums?: { id: string; title?: string }[];
  }) => void;
  onSelectAlbum?: (album: { id: string; title: string }) => void;
  onSelectArtist?: (artist: { id: string; name: string }) => void;
}

export default function SearchInput({
  placeholder = "Search...",
  className = "",
  showTracks = true,
  showAlbums = true,
  showArtists = true,
  minLen = 2,
  onSelectTrack,
  onSelectAlbum,
  onSelectArtist,
}: SearchInputProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult | null>(null);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);

  const doSearch = useCallback(
    async (q: string) => {
      if (q.trim().length < minLen) {
        setResults(null);
        return;
      }
      setLoading(true);
      try {
        const r = await api.data.search(q.trim());
        setResults(r);
        setOpen(true);
      } catch {
        setResults(null);
      } finally {
        setLoading(false);
      }
    },
    [minLen],
  );

  const onChange = (val: string) => {
    setQuery(val);
    clearTimeout(timerRef.current);
    if (val.trim().length < minLen) {
      setResults(null);
      setOpen(false);
      return;
    }
    timerRef.current = setTimeout(() => doSearch(val), 300);
  };

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      clearTimeout(timerRef.current);
      doSearch(query);
    }
  };

  const hasResults =
    results &&
    ((showTracks && results.tracks?.length > 0) ||
      (showAlbums && results.albums?.length > 0) ||
      (showArtists && results.artists?.length > 0));

  return (
    <div ref={containerRef} className={`relative ${className}`}>
      <input
        type="text"
        placeholder={placeholder}
        value={query}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        onFocus={() => {
          if (results) setOpen(true);
        }}
        className="w-full px-3 py-1.5 text-sm bg-zinc-800 text-zinc-300 border border-zinc-700 rounded-lg outline-none focus:border-green-500 placeholder-zinc-500"
      />
      {open && results && (
        <div className="absolute top-full left-0 right-0 mt-1 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-50 py-1 max-h-72 overflow-y-auto">
          {showTracks &&
            results.tracks?.map((track) => (
              <button
                key={"t-" + track.id}
                onClick={() => {
                  onSelectTrack?.(track);
                  setOpen(false);
                  setQuery("");
                }}
                className="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 hover:bg-zinc-700 cursor-pointer"
              >
                <Music className="w-3.5 h-3.5 text-green-500 shrink-0" />
                <span className="flex-1 min-w-0 truncate">{track.title}</span>
                <span className="text-xs text-zinc-500 shrink-0">{t("search.track")}</span>
              </button>
            ))}
          {showAlbums &&
            results.albums?.map((album) => (
              <button
                key={"a-" + album.id}
                onClick={() => {
                  onSelectAlbum?.(album);
                  setOpen(false);
                  setQuery("");
                }}
                className="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 hover:bg-zinc-700 cursor-pointer"
              >
                <Disc3 className="w-3.5 h-3.5 text-blue-400 shrink-0" />
                <span className="flex-1 min-w-0 truncate">{album.title}</span>
                <span className="text-xs text-zinc-500 shrink-0">{t("search.album")}</span>
              </button>
            ))}
          {showArtists &&
            results.artists?.map((artist) => (
              <button
                key={"r-" + artist.id}
                onClick={() => {
                  onSelectArtist?.(artist);
                  setOpen(false);
                  setQuery("");
                }}
                className="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 hover:bg-zinc-700 cursor-pointer"
              >
                <Mic2 className="w-3.5 h-3.5 text-yellow-400 shrink-0" />
                <span className="flex-1 min-w-0 truncate">{artist.name}</span>
                <span className="text-xs text-zinc-500 shrink-0">{t("search.artist")}</span>
              </button>
            ))}
          {!hasResults && (
            <div className="py-3 text-center text-sm text-zinc-500">
              {loading ? t("search.searching") : t("search.noResults")}
            </div>
          )}
        </div>
      )}
      {open && !results && query.trim().length >= minLen && (
        <div className="absolute top-full left-0 right-0 mt-1 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-50 py-3 text-center text-sm text-zinc-500">
          {loading ? t("search.searching") : t("search.noResults")}
        </div>
      )}
    </div>
  );
}
