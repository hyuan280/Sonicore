import { RefreshCw } from "lucide-react";

// PluginRefreshButton is the shared toolbar refresh control used by the
// plugin tabs (market / installed / uninstalled). It re-runs the tab's own
// load function and shows a spinner while busy.
export function PluginRefreshButton({
  label,
  busyLabel,
  busy,
  onClick,
}: {
  label: string;
  // Optional label shown while busy (e.g. "Syncing..."); falls back to label.
  busyLabel?: string;
  busy: boolean;
  onClick: () => void;
}) {
  const text = busy && busyLabel ? busyLabel : label;
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      aria-label={text}
      title={text}
      className="p-2 rounded-lg cursor-pointer transition-colors text-zinc-400 hover:text-white hover:bg-zinc-800 disabled:opacity-50 disabled:cursor-not-allowed"
    >
      <RefreshCw className={`w-4 h-4 ${busy ? "animate-spin" : ""}`} />
    </button>
  );
}
