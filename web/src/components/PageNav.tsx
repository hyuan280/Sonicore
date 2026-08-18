import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, ChevronDown } from "lucide-react";
import { useState } from "react";

interface PageNavProps {
  page: number;
  totalPages: number;
  total: number;
  perPage: number;
  onPage: (page: number) => void;
  onPerPage: (n: number) => void;
}

export default function PageNav({
  page,
  totalPages,
  total,
  perPage,
  onPage,
  onPerPage,
}: PageNavProps) {
  const { t } = useTranslation();
  const [perPageOpen, setPerPageOpen] = useState(false);
  const shownTotalPages = Math.max(totalPages, 1);
  return (
    <div className="flex items-center justify-end gap-2">
      <span className="text-sm text-zinc-400">{t("trackTable.tracks", { count: total })}</span>
      {totalPages > 1 && (
        <div className="flex items-center bg-zinc-800 rounded-lg">
          <div className="relative">
            <button
              type="button"
              aria-haspopup="menu"
              aria-expanded={perPageOpen}
              onClick={() => setPerPageOpen(!perPageOpen)}
              className="px-3 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-l-lg cursor-pointer transition-colors"
            >
              {perPage} <ChevronDown className="w-4 h-4 inline-block -m-0.5" />
            </button>
            {perPageOpen && (
              <div
                className="absolute top-full left-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1"
                onClick={() => setPerPageOpen(false)}
              >
                {[10, 20, 30, 50].map((n) => (
                  <button
                    type="button"
                    key={n}
                    onClick={() => {
                      onPerPage(n);
                      onPage(1);
                    }}
                    className={`w-full text-left px-3 py-1.5 text-sm cursor-pointer ${perPage === n ? "text-white" : "text-zinc-400 hover:text-white"}`}
                  >
                    {n}
                  </button>
                ))}
              </div>
            )}
          </div>
          <span className="w-px h-4 bg-zinc-700" />
          <span className="text-sm text-zinc-400 px-3 py-2">
            {page} / {shownTotalPages}
          </span>
          <span className="w-px h-4 bg-zinc-700" />
          <button
            type="button"
            disabled={page <= 1}
            onClick={() => onPage(page - 1)}
            aria-label={t("trackTable.prev")}
            className="flex items-center justify-center px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 disabled:opacity-30 cursor-pointer transition-colors"
          >
            <ChevronLeft className="w-4 h-4" />
          </button>
          <span className="w-px h-4 bg-zinc-700" />
          <button
            type="button"
            disabled={page >= totalPages}
            onClick={() => onPage(page + 1)}
            aria-label={t("trackTable.next")}
            className="flex items-center justify-center px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-r-lg disabled:opacity-30 cursor-pointer transition-colors"
          >
            <ChevronRight className="w-4 h-4" />
          </button>
        </div>
      )}
    </div>
  );
}
