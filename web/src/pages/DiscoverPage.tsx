import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { Card, CardGrid } from "../components/ui/card";
import PlatformSwitcher, { type PlatformItem } from "../components/PlatformSwitcher";
import { translateApiError } from "../i18n/errorCodes";
import { Music2, X, Search } from "lucide-react";

interface ChartItem {
  id: string;
  name: string;
  description?: string;
  cover_url?: string;
  track_count: number;
  update_freq?: string;
}

export default function DiscoverPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [platforms, setPlatforms] = useState<PlatformItem[]>([]);
  const [platform, setPlatform] = useState("");
  const [charts, setCharts] = useState<ChartItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [noPlatform, setNoPlatform] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const [chartError, setChartError] = useState<string | null>(null);
  const [retryKey, setRetryKey] = useState(0);
  const [query, setQuery] = useState("");

  useEffect(() => {
    let cancelled = false;
    api.platform
      .list()
      .then((d) => {
        if (cancelled) return;
        const plats = (d.platforms || []) as PlatformItem[];
        setPlatforms(plats);
        if (plats.length === 0) {
          setNoPlatform(true);
          return;
        }
        setPlatform((prev) => (plats.some((p) => p.name === prev) ? prev : plats[0].name));
      })
      .catch((err) => {
        if (!cancelled) setListError(translateApiError(t, err));
      });
    return () => {
      cancelled = true;
    };
  }, [retryKey]);

  useEffect(() => {
    if (!platform) return;
    let cancelled = false;
    setLoading(true);
    setChartError(null);
    api.platform
      .charts(platform)
      .then((d) => {
        if (cancelled) return;
        setCharts(d.charts || []);
      })
      .catch((err) => {
        if (!cancelled) {
          setCharts([]);
          setChartError(translateApiError(t, err));
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [platform, retryKey]);

  const submitSearch = () => {
    const q = query.trim();
    if (q && platform) navigate(`/discover/search/${platform}?q=${encodeURIComponent(q)}`);
  };

  const renderChartBody = () => {
    if (chartError) {
      return (
        <div className="text-center py-16 space-y-3">
          <p className="text-zinc-400">{chartError}</p>
          <button
            onClick={() => setRetryKey((k) => k + 1)}
            className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer"
          >
            {t("common.retry")}
          </button>
        </div>
      );
    }
    if (loading) {
      return (
        <div className="flex items-center justify-center py-16">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-zinc-700 border-t-green-500" />
        </div>
      );
    }
    return (
      <CardGrid>
        {charts.map((c) => (
          <Link key={c.id} to={`/discover/charts/${platform}/${c.id}`} className="block">
            <Card className="hover:bg-zinc-800/50 transition-colors h-full p-0 overflow-hidden">
              <div className="aspect-square flex items-center justify-center overflow-hidden bg-zinc-800">
                {c.cover_url ? (
                  <img
                    src={c.cover_url}
                    alt={c.name}
                    loading="lazy"
                    className="w-full h-full object-cover"
                    onError={(e) => {
                      (e.target as HTMLImageElement).style.display = "none";
                      (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden");
                    }}
                  />
                ) : null}
                <Music2 className={`w-8 h-8 text-zinc-600 ${c.cover_url ? "hidden" : ""}`} />
              </div>
              <div className="p-3">
                <p className="font-medium text-sm truncate">{c.name}</p>
                <div className="flex items-center gap-2 text-xs text-zinc-500 mt-1">
                  {c.update_freq && <span>{c.update_freq}</span>}
                  {c.track_count > 0 && (
                    <span>· {t("album.tracks", { count: c.track_count })}</span>
                  )}
                </div>
              </div>
            </Card>
          </Link>
        ))}
      </CardGrid>
    );
  };

  if (noPlatform) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center space-y-3">
          <Music2 className="w-10 h-10 text-zinc-600 mx-auto" />
          <p className="text-zinc-400">{t("discover.noPlatform")}</p>
        </div>
      </div>
    );
  }

  if (listError) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center space-y-3">
          <Music2 className="w-10 h-10 text-zinc-600 mx-auto" />
          <p className="text-zinc-400">{listError}</p>
          <button
            onClick={() => {
              setListError(null);
              setRetryKey((k) => k + 1);
            }}
            className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer"
          >
            {t("common.retry")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="sticky top-0 z-10 bg-black px-6 pt-6 pb-4 space-y-3">
        <div className="relative flex items-center">
          <h1 className="text-2xl font-bold shrink-0">{t("nav.discover")}</h1>
          <div className="flex-1" />
          <div className="relative max-w-md w-full">
            <input
              type="text"
              value={query}
              placeholder={t("discover.searchPlaceholder")}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") submitSearch();
              }}
              className="w-full px-3 py-1.5 pr-16 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
            />
            {query && (
              <button
                onClick={() => setQuery("")}
                className="absolute right-8 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer"
              >
                <X className="w-4 h-4" />
              </button>
            )}
            <button
              onClick={submitSearch}
              className="absolute right-1 top-1/2 -translate-y-1/2 p-1.5 text-zinc-400 hover:text-green-500 cursor-pointer"
            >
              <Search className="w-4 h-4" />
            </button>
          </div>
        </div>
        <PlatformSwitcher platforms={platforms} platform={platform} onChange={setPlatform} />
      </div>

      <div className="px-6 pb-24 space-y-6">
        {renderChartBody()}
        {platform && !loading && !chartError && charts.length === 0 && (
          <p className="text-zinc-500 text-center py-12">{t("discover.noCharts")}</p>
        )}
      </div>
    </>
  );
}
