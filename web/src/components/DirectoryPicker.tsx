import { useState, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../i18n/errorCodes";
import { api } from "../api/client";
import { Folder, ChevronRight, ArrowUp, Loader2 } from "lucide-react";

interface DirEntry {
  name: string;
  path: string;
}

interface DirData {
  current: string;
  parent: string;
  dirs: DirEntry[];
  has_parent: boolean;
}

interface Props {
  open: boolean;
  initialPath?: string;
  onClose: () => void;
  onSelect: (path: string) => void;
}

function parentOf(p: string): string {
  const s = p.endsWith("/") ? p.slice(0, -1) : p;
  const i = s.lastIndexOf("/");
  return i > 0 ? s.slice(0, i) : "/";
}

export default function DirectoryPicker({ open, initialPath, onClose, onSelect }: Props) {
  const { t } = useTranslation();
  const [data, setData] = useState<DirData | null>(null);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  function load(dir: string) {
    setLoading(true);
    setError("");
    api.admin
      .dirs(dir)
      .then((d) => {
        setData(d);
        if (d) setInput(d.current || "");
      })
      .catch((err: any) => {
        setError(translateApiError(t, err));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    if (!open) return;
    setInput("");
    setError("");
    setData(null);
    load(initialPath || "");
  }, [open]);

  useEffect(() => {
    if (open && inputRef.current) inputRef.current.focus();
  }, [open, data]);

  const dirs = data?.dirs ?? [];

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
    >
      <div
        className="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-lg mx-4 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-4 border-b border-zinc-800">
          <h3 className="font-medium mb-2">Select Music Directory</h3>
          <div className="flex gap-2">
            <input
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") load(input);
              }}
              placeholder="/opt/sonicore/music"
              className="flex-1 bg-zinc-800 text-sm text-white rounded-lg px-3 py-2 outline-none focus:ring-1 focus:ring-green-500"
            />
            <button
              onClick={() => load(input)}
              className="px-3 py-2 bg-green-600 hover:bg-green-700 rounded-lg text-sm cursor-pointer"
            >
              Browse
            </button>
          </div>
          {error && <p className="text-red-400 text-xs mt-1">{error}</p>}
        </div>

        <div className="h-64 overflow-y-auto p-2 space-y-0.5 relative">
          {!data && loading && (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="w-6 h-6 animate-spin text-zinc-500" />
            </div>
          )}
          {!data && !loading && error && (
            <div className="flex items-center justify-center h-full">
              <p className="text-zinc-500 text-sm">{error}</p>
            </div>
          )}
          {data && (
            <>
              <button
                onClick={() => load(parentOf(data.current || data.parent))}
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer"
              >
                <ArrowUp className="w-4 h-4" />
                ..
              </button>
              {loading && (
                <div className="absolute inset-0 bg-black/30 flex items-start justify-center pt-12 rounded-b-xl">
                  <Loader2 className="w-5 h-5 animate-spin text-green-500" />
                </div>
              )}
              {dirs.length === 0 && !loading ? (
                <p className="text-zinc-500 text-sm text-center py-8">(empty)</p>
              ) : (
                dirs.map((d) => (
                  <div
                    key={d.path}
                    className="flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-zinc-800 group cursor-pointer"
                  >
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setInput(d.path);
                        load(d.path + "/");
                      }}
                      className="shrink-0 p-0.5 rounded text-zinc-500 hover:text-yellow-400 hover:bg-zinc-700 cursor-pointer"
                    >
                      <Folder className="w-4 h-4" />
                    </button>
                    <span
                      className="text-sm flex-1 truncate"
                      onClick={() => {
                        onSelect(d.path + "/");
                        onClose();
                      }}
                    >
                      {d.name}
                    </span>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setInput(d.path);
                        load(d.path + "/");
                      }}
                      className="p-0.5 rounded text-zinc-500 hover:text-white hover:bg-zinc-700 cursor-pointer"
                    >
                      <ChevronRight className="w-4 h-4" />
                    </button>
                  </div>
                ))
              )}
            </>
          )}
        </div>

        <div className="p-3 border-t border-zinc-800 flex justify-end gap-2">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-zinc-400 hover:text-white cursor-pointer"
          >
            Cancel
          </button>
          <button
            onClick={() => {
              const v = input.endsWith("/") ? input : input + "/";
              if (input) {
                onSelect(v);
                onClose();
              }
            }}
            className="px-4 py-2 bg-green-600 hover:bg-green-700 rounded-lg text-sm cursor-pointer"
          >
            Select
          </button>
        </div>
      </div>
    </div>
  );
}
