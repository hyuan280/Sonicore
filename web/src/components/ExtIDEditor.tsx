import { Search, Loader2 } from "lucide-react";

interface ExtIDEditorProps {
  extIDValue: string;
  setExtIDValue: (value: string) => void;
  extIDSource: string;
  setExtIDSource: (value: string) => void;
  extIDError: boolean;
  setExtIDError: (value: boolean) => void;
  extIDSearching: boolean;
  handleExtIDSearch: () => void;
  availableSources: { name: string; label: string }[];
  orig: string;
  placeholder?: string;
}

export default function ExtIDEditor({
  extIDValue,
  setExtIDValue,
  extIDSource,
  setExtIDSource,
  extIDError,
  setExtIDError,
  extIDSearching,
  handleExtIDSearch,
  availableSources,
  orig,
  placeholder,
}: ExtIDEditorProps) {
  return (
    <div className="flex items-center flex-1 min-w-0">
      <select
        value={extIDSource || availableSources[0]?.name || ""}
        onChange={(e) => {
          if (e.target.value !== extIDSource && extIDValue === orig) {
            setExtIDValue("");
          }
          setExtIDSource(e.target.value);
          setExtIDError(false);
        }}
        className="bg-zinc-800 rounded-l px-2 py-0.5 text-sm border-r border-zinc-700 focus:outline-none focus:ring-1 focus:ring-green-500 shrink-0 min-w-0"
      >
        {availableSources.map((s) => (
          <option key={s.name} value={s.name}>
            {s.label}
          </option>
        ))}
      </select>
      <input
        value={extIDValue}
        onChange={(e) => {
          setExtIDValue(e.target.value);
          setExtIDError(false);
        }}
        className={`bg-zinc-800 px-2 py-0.5 text-sm flex-1 min-w-0 font-mono focus:outline-none focus:ring-1 ${extIDError ? "focus:ring-red-500 border border-red-500" : "focus:ring-green-500"}`}
        placeholder={placeholder}
        onKeyDown={(e) => e.key === "Enter" && handleExtIDSearch()}
      />
      <button
        onClick={handleExtIDSearch}
        disabled={extIDSearching || !extIDValue}
        className="rounded-r px-2 py-0.5 bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50 border-l border-zinc-700 shrink-0"
      >
        {extIDSearching ? (
          <Loader2 className="w-4 h-4 animate-spin" />
        ) : (
          <Search className="w-4 h-4" />
        )}
      </button>
    </div>
  );
}
