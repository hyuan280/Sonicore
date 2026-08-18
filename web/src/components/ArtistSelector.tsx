import { useState } from "react";
import { Search, X, Plus, Loader2, Check } from "lucide-react";

export interface SelectedArtist {
  name: string;
  external_id: string;
}

interface Props {
  artists: SelectedArtist[];
  onChange: (artists: SelectedArtist[]) => void;
  showAdd?: boolean;
  onAddToggle?: (open: boolean) => void;
}

export default function ArtistSelector({ artists, onChange, showAdd, onAddToggle }: Props) {
  const [showSearch, setShowSearch] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [results, setResults] = useState<SelectedArtist[]>([]);
  const [searching, setSearching] = useState(false);
  const [searched, setSearched] = useState(false);
  const [lastQuery, setLastQuery] = useState("");

  const doSearch = async () => {
    const q = searchQuery.trim();
    if (!q) return;
    setLastQuery(q);
    setSearching(true);
    setSearched(false);
    try {
      const res = await fetch("/api/metadata/search/artist", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + localStorage.getItem("token"),
        },
        body: JSON.stringify({ name: q }),
      });
      const data = await res.json();
      const list = (data.artists || []).filter(
        (a: any) => a.name.toLowerCase() === q.toLowerCase(),
      );
      setResults(list);
      setSearched(true);
    } catch {
      setResults([]);
      setSearched(true);
    }
    setSearching(false);
  };

  const selectArtist = (artist: SelectedArtist) => {
    if (!artists.find((a) => a.name === artist.name)) {
      onChange([...artists, artist]);
    }
    setShowSearch(false);
    setSearchQuery("");
    setResults([]);
    setSearched(false);
    setLastQuery("");
    onAddToggle?.(false);
  };

  const selectUnmatched = () => {
    if (!lastQuery) return;
    selectArtist({ name: lastQuery, external_id: "" });
  };

  const removeArtist = (idx: number) => {
    onChange(artists.filter((_, i) => i !== idx));
  };

  const open = () => {
    setShowSearch(true);
    onAddToggle?.(true);
  };
  const close = () => {
    setShowSearch(false);
    setResults([]);
    setSearched(false);
    setLastQuery("");
    onAddToggle?.(false);
  };

  const showCreate = searched && !searching && results.length === 0 && lastQuery;

  return (
    <div className="flex-1 min-w-0 bg-zinc-800 rounded-lg p-1.5 space-y-1">
      {artists.map((a, i) => (
        <div key={i} className="flex items-center gap-1 text-sm w-full">
          <span className="text-zinc-200 truncate flex-1 min-w-0">{a.name}</span>
          {a.external_id ? (
            <span className="text-xs text-zinc-500 font-mono shrink-0">
              {a.external_id.substring(0, 8)}
            </span>
          ) : (
            <span className="shrink-0" />
          )}
          <button
            onClick={() => removeArtist(i)}
            className="p-0.5 text-zinc-500 hover:text-red-400 cursor-pointer shrink-0"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      ))}
      {showSearch ? (
        <div className="space-y-1 border border-zinc-700 rounded-lg p-2 w-full">
          <div className="flex gap-1">
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") doSearch();
              }}
              placeholder="Search artist..."
              className="bg-zinc-800 rounded px-2 py-1 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
              autoFocus
            />
            <button
              onClick={doSearch}
              disabled={searching}
              className="p-1 text-zinc-400 hover:text-white cursor-pointer"
            >
              <Search className="w-4 h-4" />
            </button>
            <button onClick={close} className="p-1 text-zinc-500 hover:text-white cursor-pointer">
              <X className="w-4 h-4" />
            </button>
          </div>
          {searching && (
            <div className="text-center">
              <Loader2 className="w-4 h-4 animate-spin inline text-zinc-400" />
            </div>
          )}
          {results.map((r, i) => (
            <button
              key={i}
              onClick={() => selectArtist(r)}
              className="w-full flex items-center gap-1 px-2 py-1 rounded text-left text-sm hover:bg-zinc-700 cursor-pointer"
            >
              <span className="text-zinc-200 truncate min-w-0">{r.name}</span>
              <Check className="w-3.5 h-3.5 text-green-500 shrink-0" />
              <span className="text-xs text-zinc-500 font-mono ml-auto shrink-0">
                {r.external_id ? r.external_id.substring(0, 8) : ""}
              </span>
            </button>
          ))}
          {showCreate && (
            <button
              onClick={selectUnmatched}
              className="w-full flex items-center gap-1 px-2 py-1 rounded text-left text-sm hover:bg-zinc-700 cursor-pointer"
            >
              <span className="text-zinc-200 truncate min-w-0">{lastQuery}</span>
              <X className="w-3 h-3 text-zinc-500 shrink-0" />
              <span className="text-xs text-green-400 ml-auto shrink-0">click to select</span>
            </button>
          )}
        </div>
      ) : (
        showAdd && (
          <button
            onClick={open}
            className="text-sm text-green-400 hover:text-green-300 cursor-pointer flex items-center gap-1"
          >
            <Plus className="w-3 h-3" /> Add
          </button>
        )
      )}
    </div>
  );
}
