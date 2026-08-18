import { useTranslation } from "react-i18next";
import type { ReactNode } from "react";
import { Card } from "./ui/card";
import { Input } from "./ui/input";

interface MetadataProviderCardProps {
  icon: ReactNode;
  title: string;
  enableLabel: string;
  enableDesc: string;
  enabled: boolean;
  onEnabledChange: (next: boolean) => void;
  rateLimit: string;
  onRateLimitChange: (next: string) => void;
  saving: boolean;
  modified: boolean;
  onSave: () => void;
  onRevert: () => void;
  error?: string;
  children?: ReactNode;
}

export function MetadataProviderCard({
  icon,
  title,
  enableLabel,
  enableDesc,
  enabled,
  onEnabledChange,
  rateLimit,
  onRateLimitChange,
  saving,
  modified,
  onSave,
  onRevert,
  error,
  children,
}: MetadataProviderCardProps) {
  const { t } = useTranslation();
  return (
    <Card className="space-y-3">
      <h3 className="font-medium flex items-center gap-2">
        {icon} {title}
      </h3>
      <div className="space-y-3 p-3 rounded-lg bg-zinc-800/50">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium">{enableLabel}</p>
            <p className="text-xs text-zinc-400">{enableDesc}</p>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={enabled}
            aria-label={enableLabel}
            onClick={() => onEnabledChange(!enabled)}
            disabled={saving}
            className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer disabled:opacity-50 ${enabled ? "bg-green-600" : "bg-zinc-700"}`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${enabled ? "translate-x-6" : ""}`}
            />
          </button>
        </div>
        {children}
        <div>
          <p className="text-xs text-zinc-400 mb-1">{t("admin.rateLimit")}</p>
          <Input
            value={rateLimit}
            onChange={(e) => onRateLimitChange(e.target.value)}
            placeholder="1"
            disabled={saving}
          />
        </div>
        {error && <span className="text-xs text-red-400">{error}</span>}
        {modified && (
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onSave}
              disabled={saving}
              className="px-3 py-1.5 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer"
            >
              {saving ? t("admin.saving") : t("admin.save")}
            </button>
            <button
              type="button"
              onClick={onRevert}
              disabled={saving}
              className="px-3 py-1.5 rounded-lg text-sm bg-zinc-700 text-white hover:bg-zinc-600 disabled:opacity-50 cursor-pointer"
            >
              {t("admin.revert")}
            </button>
          </div>
        )}
      </div>
    </Card>
  );
}
