import type { ReactNode } from "react";
import { Card } from "../../components/ui/card";
import { DropdownMenu } from "../../components/ui/menu";
import { Puzzle, Download, ShieldCheck, ShieldQuestion, MoreVertical } from "lucide-react";
import { cn } from "../../lib/utils";
import type { PluginRepo, PluginStatus } from "../../types";

export type VerifiedKind = "official" | "third_party" | "unverified";

const STATUS_COLORS: Record<PluginStatus, string> = {
  ok: "bg-green-500",
  disabled: "bg-zinc-600",
  error: "bg-red-500",
};

function verifiedIcon(kind: VerifiedKind) {
  if (kind === "official") return <ShieldCheck className="w-3 h-3 text-green-500 shrink-0" />;
  if (kind === "third_party") return <ShieldCheck className="w-3 h-3 text-zinc-500 shrink-0" />;
  return <ShieldQuestion className="w-3 h-3 text-yellow-500 shrink-0" />;
}

// buildRepoOfficialMap turns the /repos response into a repo-name → official
// map, used by the installed/uninstalled tabs to derive the verified badge.
export function buildRepoOfficialMap(
  res: { repos?: PluginRepo[] } | undefined,
): Map<string, boolean> {
  const map = new Map<string, boolean>();
  for (const r of res?.repos || []) {
    map.set(r.name, !!r.official);
  }
  return map;
}

// sourceInfo classifies a plugin's source field: local/manual plugins are
// unverified and labeled "local", while a repo name is verified (official or
// third-party depending on the configured repo).
export function sourceInfo(
  source: string,
  repoOfficial: boolean | undefined,
): { kind: VerifiedKind; isLocal: boolean } {
  if (source !== "" && source !== "local" && source !== "manual") {
    if (repoOfficial === undefined) {
      return { kind: "unverified", isLocal: false };
    }
    return { kind: repoOfficial ? "official" : "third_party", isLocal: false };
  }
  return { kind: "unverified", isLocal: true };
}

export interface PluginCardProps {
  name: string;
  version: string;
  // verified + sourceLabel render the "是否验证" badge: the shield icon is
  // derived from verified, and the label next to it is provided by the tab
  // (the market shows the repo name, the installed tab shows 官方/第三方/未验证).
  verified: VerifiedKind;
  sourceLabel: string;
  status?: PluginStatus;
  statusLabel?: string;
  description?: string;
  author?: string;
  downloads?: number;
  badge?: ReactNode;
  menuLabel?: string;
  menuItems?: ReactNode;
  onClick?: () => void;
}

export function PluginCard({
  name,
  version,
  verified,
  sourceLabel,
  status,
  statusLabel,
  description,
  author,
  downloads,
  badge,
  menuLabel,
  menuItems,
  onClick,
}: PluginCardProps) {
  return (
    <Card
      className={cn(
        "flex flex-col pb-0.5",
        onClick &&
          "cursor-pointer hover:border-zinc-700 transition-all duration-200 hover:scale-[1.03]",
      )}
      onClick={onClick}
    >
      <div className="flex items-start gap-3 mb-2">
        <div className="w-12 h-12 rounded-lg bg-zinc-800/60 flex items-center justify-center shrink-0">
          <Puzzle className="w-6 h-6 text-green-500" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 min-w-0">
            <span className="font-semibold truncate flex-1">{name}</span>
            {badge}
            {status && (
              <span
                role="status"
                title={statusLabel}
                aria-label={statusLabel}
                className={cn("w-2.5 h-2.5 rounded-full shrink-0", STATUS_COLORS[status])}
              />
            )}
          </div>
          <div className="flex items-center gap-2 text-xs text-zinc-500 mt-0.5">
            <span>v{version}</span>
            <span>·</span>
            <span className="flex items-center gap-1 truncate">
              {verifiedIcon(verified)}
              {sourceLabel}
            </span>
          </div>
        </div>
      </div>
      {description && <p className="text-xs text-zinc-500 line-clamp-2 mb-2">{description}</p>}
      <div className="mt-auto border-t border-zinc-800 pt-0.5 flex items-center gap-2">
        <span className="text-xs text-zinc-400 truncate flex-1">{author || "—"}</span>
        {downloads !== undefined && (
          <span className="flex items-center gap-1 text-xs text-zinc-500 shrink-0">
            <Download className="w-3 h-3" />
            {downloads}
          </span>
        )}
        {menuItems !== null && menuItems !== undefined && (
          <DropdownMenu
            trigger={(toggle, open, ariaProps) => (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  toggle();
                }}
                {...ariaProps}
                aria-label={menuLabel}
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
            {menuItems}
          </DropdownMenu>
        )}
      </div>
    </Card>
  );
}
