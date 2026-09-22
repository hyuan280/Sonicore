import { Flame } from "lucide-react";
import { useTranslation } from "react-i18next";

// Heat colour ramp: 0 is neutral gray, 100 (and above) is the most saturated
// red. Values are clamped so heat > 100 does not overflow the ramp.
const GRAY = [113, 113, 122];
const RED = [239, 68, 68];

function heatColor(heat: number): string {
  const t = Math.max(0, Math.min(1, heat / 100));
  const r = Math.round(GRAY[0] + (RED[0] - GRAY[0]) * t);
  const g = Math.round(GRAY[1] + (RED[1] - GRAY[1]) * t);
  const b = Math.round(GRAY[2] + (RED[2] - GRAY[2]) * t);
  return `rgb(${r}, ${g}, ${b})`;
}

interface HeatBadgeProps {
  heat?: number;
  className?: string;
}

export default function HeatBadge({ heat, className = "" }: HeatBadgeProps) {
  const { t } = useTranslation();
  // Missing heat means "not loaded", not "zero" — render nothing so a loading
  // queue or a response without the field is not mislabelled as a real 0.
  if (heat === undefined) return null;

  const color = heatColor(heat);
  return (
    <span
      className={`inline-flex items-center gap-0.5 shrink-0 text-xs font-medium tabular-nums ${className}`}
      style={{ color }}
      title={`${t("songs.heat")}: ${heat}`}
    >
      <Flame className="w-3.5 h-3.5" style={{ color }} />
      {heat}
    </span>
  );
}
