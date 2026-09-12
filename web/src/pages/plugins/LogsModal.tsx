import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal } from "../../components/ui/modal";
import { Input } from "../../components/ui/input";
import { pluginLogsStream } from "../../api/client";
import { Loader2, Pause, Play, RefreshCw } from "lucide-react";

const MAX_RECORDS = 5000;

type LevelFilter = "all" | "D" | "I" | "W" | "E";

interface LogRecord {
  ts: string;
  level: string;
  text: string;
}

// parseRecord splits one log line into time/level/message. Returns null
// when the line is not a record start (a continuation of a multi-line
// message) — the assembler appends those to the previous record.
function parseRecord(raw: string): LogRecord | null {
  const m = raw.match(/^(\S+)\s+([DIWE])\s+([\s\S]*)$/);
  if (!m) {
    return null;
  }
  return { ts: m[1], level: m[2], text: m[3] };
}

// LogsModal shows one plugin's live log stream (SSE): the server sends the
// last 50 records' lines and then follows the file line by line. The
// assembler rebuilds multi-line records (a non-record-start line extends
// the previous one); level filtering and content search happen client-side
// over the assembled records (capped at 5000).
export default function LogsModal({
  plugin,
  onClose,
}: {
  plugin: { id: string; name: string };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [records, setRecords] = useState<LogRecord[]>([]);
  const [level, setLevel] = useState<LevelFilter>("all");
  const [query, setQuery] = useState("");
  const [follow, setFollow] = useState(true);
  const [paused, setPaused] = useState(false);
  const [status, setStatus] = useState<"connecting" | "live" | "closed" | "paused">("connecting");
  const [error, setError] = useState("");
  const [reconnectKey, setReconnectKey] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  // everLive marks that a stream delivered data at least once; only then is
  // auto-reconnect enabled (a failing first connection stays closed).
  const everLiveRef = useRef(false);
  // lastTsRef is the resume cursor: the timestamp of the last record the
  // client saw. Reconnects pass it so lines written while disconnected /
  // paused are replayed instead of lost.
  const lastTsRef = useRef("");
  // tailLinesRef mirrors the raw lines of the buffer tail; on a resume
  // reconnect it becomes the dedupe set (the server replays from the first
  // cursor-timestamp line, which overlaps the buffer).
  const tailLinesRef = useRef<string[]>([]);
  const dedupeSetRef = useRef<Set<string> | null>(null);
  // autoRetryRef allows exactly one automatic reconnect per disconnection;
  // when that retry errors too, the stream stays closed for the manual
  // button.
  const autoRetryRef = useRef(false);
  const retryTimer = useRef<number | null>(null);

  useEffect(() => {
    setRecords([]);
    everLiveRef.current = false;
    lastTsRef.current = "";
    tailLinesRef.current = [];
    dedupeSetRef.current = null;
    autoRetryRef.current = false;
    setPaused(false);
  }, [plugin.id]);

  useEffect(() => {
    if (paused) {
      setStatus("paused");
      return;
    }
    const ctrl = new AbortController();
    let cancelled = false;
    setStatus("connecting");
    setError("");
    // Resumed connections dedupe the replay overlap: the server sends from
    // the first cursor-timestamp line, which may repeat buffered lines.
    dedupeSetRef.current = lastTsRef.current ? new Set(tailLinesRef.current) : null;

    pluginLogsStream(
      plugin.id,
      {
        onOpen: () => {
          if (cancelled) return;
          setStatus("live");
          everLiveRef.current = true;
          autoRetryRef.current = false;
        },
        onLine: (raw) => {
          if (cancelled) return;
          setStatus("live");
          if (raw === "") return;
          if (dedupeSetRef.current) {
            if (dedupeSetRef.current.delete(raw)) return;
            // First non-overlapping line: dedupe is done for good.
            dedupeSetRef.current = null;
          }
          tailLinesRef.current = [...tailLinesRef.current, raw].slice(-500);
          const rec = parseRecord(raw);
          if (rec) {
            // A new record: commit it as its own row and advance the
            // resume cursor.
            lastTsRef.current = rec.ts;
            setRecords((prev) => {
              const next =
                prev.length >= MAX_RECORDS ? prev.slice(prev.length - MAX_RECORDS + 1) : [...prev];
              next.push(rec);
              return next;
            });
          } else {
            // Continuation: extend the previous row (headless when the
            // stream started mid-message).
            setRecords((prev) => {
              if (prev.length === 0) {
                return [{ ts: "", level: "", text: raw }];
              }
              const next = [...prev];
              const last = { ...next[next.length - 1] };
              last.text = last.text ? last.text + "\n" + raw : raw;
              next[next.length - 1] = last;
              return next;
            });
          }
        },
        onGap: () => {
          // The cursor rotated away: mark the skipped span with a
          // placeholder row. Dedupe is pointless from here on (the server
          // follows from the end).
          dedupeSetRef.current = null;
          if (cancelled) return;
          setRecords((prev) => [...prev, { ts: "", level: "gap", text: t("plugins.logGap") }]);
        },
        onError: (message) => {
          if (!cancelled) setError(message);
        },
      },
      ctrl.signal,
      lastTsRef.current || undefined,
    )
      .catch((err: unknown) => {
        // Surface the failure reason (network / HTTP status) instead of
        // only showing the generic "disconnected" banner.
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      })
      .then(() => {
        if (cancelled) return;
        setStatus("closed");
        // One automatic reconnect per disconnection, only when a stream
        // delivered data before; if that retry errors too it stays closed
        // for the manual button.
        if (everLiveRef.current && !autoRetryRef.current) {
          autoRetryRef.current = true;
          retryTimer.current = window.setTimeout(() => setReconnectKey((k) => k + 1), 2000);
        }
      });

    return () => {
      cancelled = true;
      ctrl.abort();
      if (retryTimer.current !== null) {
        clearTimeout(retryTimer.current);
        retryTimer.current = null;
      }
    };
  }, [plugin.id, reconnectKey, paused]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return records.filter((r) => {
      if (level !== "all" && r.level !== level) return false;
      if (q && !`${r.ts} ${r.text}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [records, level, query]);

  useEffect(() => {
    if (!follow || status !== "live") return;
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [filtered, follow, status]);

  // renderStatusButton maps the stream status to the single control
  // button (pause / resume / reconnect) without nested ternaries.
  const renderStatusButton = () => {
    if (status === "paused") {
      return (
        <button
          type="button"
          title={t("plugins.logResume")}
          onClick={() => setPaused(false)}
          className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer shrink-0"
        >
          <Play className="w-4 h-4" />
        </button>
      );
    }
    if (status === "closed") {
      return (
        <button
          type="button"
          title={t("plugins.logReconnect")}
          onClick={() => setReconnectKey((k) => k + 1)}
          className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer shrink-0"
        >
          <RefreshCw className="w-4 h-4" />
        </button>
      );
    }
    return (
      <button
        type="button"
        title={t("plugins.logPause")}
        onClick={() => setPaused(true)}
        className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer shrink-0"
      >
        <Pause className="w-4 h-4" />
      </button>
    );
  };

  return (
    <Modal
      title={`${plugin.name} ${t("plugins.viewLogs")}`}
      onClose={onClose}
      className="max-w-3xl"
    >
      <div className="space-y-3">
        <div className="flex items-center gap-2 flex-wrap">
          <select
            value={level}
            onChange={(e) => setLevel(e.target.value as LevelFilter)}
            className="rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-white focus:outline-none focus:border-green-500 cursor-pointer"
          >
            <option value="all">{t("plugins.logLevelAll")}</option>
            <option value="D">{t("plugins.logLevelDebug")}</option>
            <option value="I">{t("plugins.logLevelInfo")}</option>
            <option value="W">{t("plugins.logLevelWarn")}</option>
            <option value="E">{t("plugins.logLevelError")}</option>
          </select>
          <Input
            placeholder={t("plugins.logSearchPlaceholder")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="flex-1 min-w-40"
          />
          {renderStatusButton()}
          <label className="flex items-center gap-1.5 text-xs text-zinc-400 cursor-pointer">
            <input
              type="checkbox"
              checked={follow}
              onChange={(e) => setFollow(e.target.checked)}
              className="accent-green-600 w-3.5 h-3.5"
            />
            {t("plugins.logFollow")}
          </label>
        </div>

        <div
          ref={listRef}
          className="h-80 overflow-y-auto rounded-lg bg-black/40 border border-zinc-800"
        >
          <table className="w-full text-xs font-mono">
            <tbody>
              {status === "connecting" && (
                <tr>
                  <td colSpan={3} className="px-2 py-3 text-zinc-500">
                    <span className="flex items-center gap-2">
                      <Loader2 className="w-4 h-4 animate-spin" />
                      {t("plugins.logConnecting")}
                    </span>
                  </td>
                </tr>
              )}
              {status !== "connecting" && filtered.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-2 py-3 text-zinc-500">
                    {t("plugins.logEmpty")}
                  </td>
                </tr>
              )}
              {filtered.map((r, i) =>
                r.level === "gap" ? (
                  <tr key={i} className="border-b border-zinc-800/40">
                    <td colSpan={3} className="py-1 px-2 text-zinc-600 italic text-[11px]">
                      {r.text}
                    </td>
                  </tr>
                ) : (
                  <tr key={i} className="align-top border-b border-zinc-800/40">
                    <td className="py-1 px-2 whitespace-nowrap text-zinc-500 align-top">{r.ts}</td>
                    <td className="py-1 px-2 text-zinc-400 align-top">{r.level}</td>
                    <td className="py-1 px-2 whitespace-pre-wrap break-all">{r.text}</td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>

        {status === "closed" && (
          <p className="text-xs text-red-400">{error || t("plugins.logDisconnected")}</p>
        )}
      </div>
    </Modal>
  );
}
