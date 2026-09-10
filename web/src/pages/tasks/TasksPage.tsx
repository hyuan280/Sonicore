import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { api } from "../../api/client";
import { Button } from "../../components/ui/button";
import { translateApiError } from "../../i18n/errorCodes";
import type { ScheduledTask } from "../../types";
import { Ban, CircleCheck, Loader2, Play } from "lucide-react";

const POLL_MS = 5000;

const statusClasses: Record<ScheduledTask["status"], string> = {
  idle: "bg-green-600/20 text-green-500",
  running: "bg-blue-500/20 text-blue-400 animate-pulse",
  disabled: "bg-zinc-800 text-zinc-500",
};

// Relative time to the next run: seconds when under a minute, otherwise
// hours/minutes (e.g. "3小时20分钟").
function formatRelative(t: TFunction, iso: string | null | undefined): string {
  if (!iso) return "—";
  const diff = new Date(iso).getTime() - Date.now();
  if (diff <= 1000) return t("tasks.dueNow");
  const totalMin = Math.floor(diff / 60000);
  if (totalMin < 1) {
    return t("tasks.relativeSeconds", { count: Math.floor(diff / 1000) });
  }
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  if (h > 0 && m > 0) return t("tasks.relativeHoursMinutes", { count: h, minutes: m });
  if (h > 0) return t("tasks.relativeHours", { count: h });
  return t("tasks.relativeMinutes", { count: m });
}

function StatusBadge({ status }: { status: ScheduledTask["status"] }) {
  const { t } = useTranslation();
  const labels: Record<ScheduledTask["status"], string> = {
    idle: t("tasks.statusIdle"),
    running: t("tasks.statusRunning"),
    disabled: t("tasks.statusDisabled"),
  };
  return (
    <span className={`px-2 py-0.5 rounded-full text-sm font-medium ${statusClasses[status]}`}>
      {labels[status]}
    </span>
  );
}

// Known values are translated via i18n; unknown values (e.g. a future
// plugin's provider) fall back to the raw string.
function SourceLabel({ value }: { value: string }) {
  const { t } = useTranslation();
  return <>{t(`tasks.source_${value}`, { defaultValue: value })}</>;
}

function ProviderLabel({ value }: { value: string }) {
  const { t } = useTranslation();
  return <>{t(`tasks.provider_${value}`, { defaultValue: value })}</>;
}

export default function TasksPage() {
  const { t } = useTranslation();
  const [tasks, setTasks] = useState<ScheduledTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [runBusyIds, setRunBusyIds] = useState<Set<string>>(new Set());
  const [toggleBusyIds, setToggleBusyIds] = useState<Set<string>>(new Set());
  // Guards against overlapping requests: the 5s poll and the post-action
  // refreshes share one in-flight slot, so a slow response can never be
  // overwritten by an earlier one.
  const inflightRef = useRef<Promise<void> | null>(null);

  const fetchTasks = useCallback(async () => {
    try {
      const d = await api.tasks.list();
      setTasks(Array.isArray(d) ? (d as ScheduledTask[]) : []);
      setError("");
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  }, [t]);

  // load(force=true) waits out any in-flight request and then fetches fresh
  // data; the plain load() dedupes against the in-flight request. Action
  // handlers use force so the refresh always reflects the completed action.
  const load = useCallback(
    async (force = false) => {
      if (inflightRef.current) {
        if (!force) return inflightRef.current;
        try {
          await inflightRef.current;
        } catch {
          // the in-flight request reported its own error already
        }
      }
      const p = fetchTasks();
      inflightRef.current = p;
      try {
        await p;
      } finally {
        inflightRef.current = null;
      }
    },
    [fetchTasks],
  );

  useEffect(() => {
    load();
    const id = setInterval(() => {
      void load();
    }, POLL_MS);
    return () => clearInterval(id);
  }, [load]);

  const setBusy = (
    setter: React.Dispatch<React.SetStateAction<Set<string>>>,
    id: string,
    busy: boolean,
  ) => {
    setter((prev) => {
      const next = new Set(prev);
      if (busy) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const runTask = async (task: ScheduledTask) => {
    setBusy(setRunBusyIds, task.id, true);
    try {
      await api.tasks.run(task.id);
      await load(true);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setBusy(setRunBusyIds, task.id, false);
    }
  };

  const toggleEnabled = async (task: ScheduledTask) => {
    setBusy(setToggleBusyIds, task.id, true);
    try {
      await api.tasks.setEnabled(task.id, !task.enabled);
      await load(true);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setBusy(setToggleBusyIds, task.id, false);
    }
  };

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">{t("tasks.title")}</h1>
      {error && <div className="text-sm text-red-400">{error}</div>}
      {loading && tasks.length === 0 ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="w-6 h-6 animate-spin text-zinc-500" />
        </div>
      ) : tasks.length === 0 ? (
        <p className="text-zinc-500 py-8 text-center">{t("tasks.empty")}</p>
      ) : (
        <div className="rounded-lg border border-zinc-800 overflow-hidden">
          <table className="w-full text-[15px]">
            <thead>
              <tr className="bg-zinc-900 text-zinc-500">
                <th className="px-4 py-2 font-medium text-left">{t("tasks.source")}</th>
                <th className="px-4 py-2 font-medium text-left">{t("tasks.provider")}</th>
                <th className="pl-8 pr-4 py-2 font-medium text-left">{t("tasks.name")}</th>
                <th className="px-4 py-2 font-medium text-left">{t("tasks.status")}</th>
                <th className="pl-8 pr-4 py-2 font-medium text-left">{t("tasks.nextRun")}</th>
                <th className="px-4 py-2 font-medium text-center">{t("tasks.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((task) => {
                const runBusy = runBusyIds.has(task.id);
                const toggleBusy = toggleBusyIds.has(task.id);
                return (
                  <tr key={task.id} className="border-t border-zinc-800 hover:bg-zinc-900/50">
                    <td className="px-4 py-3 text-zinc-400 text-left">
                      <SourceLabel value={task.source} />
                    </td>
                    <td className="px-4 py-3 text-zinc-400 text-left">
                      <ProviderLabel value={task.provider} />
                    </td>
                    <td className="pl-8 pr-4 py-3 text-left">
                      <div className="font-medium">{task.name}</div>
                      <div className="text-xs text-zinc-500">ID: {task.id}</div>
                    </td>
                    <td className="px-4 py-3 text-left">
                      <StatusBadge status={task.status} />
                    </td>
                    <td className="pl-8 pr-4 py-3 text-zinc-300 text-left">
                      {formatRelative(t, task.next_run)}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-center gap-2">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="bg-green-600/20 text-green-400 hover:bg-green-600/30 hover:text-green-300"
                          disabled={runBusy || task.status === "running"}
                          onClick={() => runTask(task)}
                        >
                          {runBusy ? (
                            <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                          ) : (
                            <Play className="w-3.5 h-3.5 mr-1" />
                          )}
                          {t("tasks.run")}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className={
                            task.enabled
                              ? "bg-red-500/20 text-red-400 hover:bg-red-500/30 hover:text-red-300"
                              : "bg-green-600/20 text-green-400 hover:bg-green-600/30 hover:text-green-300"
                          }
                          disabled={toggleBusy}
                          onClick={() => toggleEnabled(task)}
                        >
                          {toggleBusy ? (
                            <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                          ) : task.enabled ? (
                            <Ban className="w-3.5 h-3.5 mr-1" />
                          ) : (
                            <CircleCheck className="w-3.5 h-3.5 mr-1" />
                          )}
                          {task.enabled ? t("tasks.disable") : t("tasks.enable")}
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
