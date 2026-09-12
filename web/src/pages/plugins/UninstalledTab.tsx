import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Card } from "../../components/ui/card";
import { DropdownMenu, MenuItem } from "../../components/ui/menu";
import { api } from "../../api/client";
import { Puzzle, Loader2, Trash2, MoreVertical, PackagePlus } from "lucide-react";
import type { PluginInstance } from "../../types";

// UninstalledTab lists plugins that were discovered on disk but are not
// installed yet (or were soft-uninstalled). Each card's menu offers exactly
// two actions: install (moves the plugin to the installed tab, disabled by
// default) and delete (removes the plugin files and the DB record).
export default function UninstalledTab() {
  const { t } = useTranslation();
  const [plugins, setPlugins] = useState<PluginInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());

  const load = async () => {
    try {
      const d = await api.plugins.uninstalled();
      setPlugins(d.plugins || []);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const run = async (id: string, action: () => Promise<unknown>) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
    setError("");
    try {
      await action();
      await load();
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setBusyIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  };

  return (
    <div className="space-y-4">
      {error && <p className="text-xs text-red-400">{error}</p>}

      {loading && (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      )}
      {!loading && plugins.length === 0 && (
        <p className="text-sm text-zinc-500 py-8 text-center">{t("plugins.noUninstalled")}</p>
      )}
      {!loading && plugins.length > 0 && (
        <div className="grid gap-4 grid-cols-[repeat(auto-fill,minmax(230px,1fr))]">
          {plugins.map((p) => (
            <Card key={p.id} className="flex flex-col pb-0.5">
              <div className="flex items-start gap-3 mb-2">
                <div className="w-12 h-12 rounded-lg bg-zinc-800/60 flex items-center justify-center shrink-0">
                  <Puzzle className="w-6 h-6 text-zinc-400" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="font-semibold truncate flex-1">{p.name}</span>
                  </div>
                  <div className="flex items-center gap-2 text-xs text-zinc-500 mt-0.5">
                    <span>v{p.version}</span>
                  </div>
                </div>
              </div>
              {p.description && (
                <p className="text-xs text-zinc-500 line-clamp-2 mb-2">{p.description}</p>
              )}
              <div className="mt-auto border-t border-zinc-800 pt-0.5 flex items-center gap-2">
                <span className="text-xs text-zinc-400 truncate flex-1">{p.author || "—"}</span>
                <DropdownMenu
                  trigger={(toggle, open, ariaProps) => (
                    <button
                      type="button"
                      onClick={toggle}
                      {...ariaProps}
                      aria-label={t("plugins.menuLabel")}
                      className={`p-1 rounded-lg cursor-pointer transition-colors shrink-0 ${
                        open
                          ? "bg-zinc-700 text-white"
                          : "text-zinc-400 hover:text-white hover:bg-zinc-800"
                      }`}
                    >
                      <MoreVertical className="w-4 h-4" />
                    </button>
                  )}
                >
                  <MenuItem
                    icon={<PackagePlus className="w-4 h-4" />}
                    disabled={busyIds.has(p.id)}
                    onClick={() => run(p.id, () => api.plugins.installPlugin(p.id))}
                  >
                    {t("plugins.installPlugin")}
                  </MenuItem>
                  <MenuItem
                    danger
                    icon={<Trash2 className="w-4 h-4" />}
                    disabled={busyIds.has(p.id)}
                    onClick={() => {
                      if (confirm(t("plugins.deletePluginConfirm", { name: p.name }))) {
                        run(p.id, () => api.plugins.remove(p.id));
                      }
                    }}
                  >
                    {t("plugins.deletePlugin")}
                  </MenuItem>
                </DropdownMenu>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
