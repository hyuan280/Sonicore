import { useRef, useEffect, useMemo, useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { usePlayer } from "../stores/player";
import { X, Settings, ChevronLeft, ChevronRight } from "lucide-react";
import { parseLRC, findCurrentLine } from "../lib/utils";
import {
  type LyricsSettings,
  loadLyricsSettings,
  saveLyricsSettings,
  COLOR_PRESETS,
  DIM_COLOR,
} from "../lib/lyricsSettings";

const DEFAULT_W = 380;
const MIN_W = 240;

const POS_KEY = "lyrics_widget_pos";
const SIZE_KEY = "lyrics_widget_size";

function loadPos(): { x: number; y: number } {
  try {
    const raw = localStorage.getItem(POS_KEY);
    if (raw) {
      const p = JSON.parse(raw);
      if (typeof p.x === "number" && typeof p.y === "number") return p;
    }
  } catch {}
  return { x: Math.max(20, window.innerWidth - 420), y: Math.max(80, window.innerHeight / 2 - 80) };
}

function loadSize(): number {
  try {
    const raw = localStorage.getItem(SIZE_KEY);
    if (raw) {
      const s = JSON.parse(raw);
      if (typeof s.w === "number") return Math.max(MIN_W, s.w);
    }
  } catch {}
  return DEFAULT_W;
}

interface Props {
  onClose: () => void;
}

type ResizeDir = "e";

export default function LyricsPanel({ onClose }: Props) {
  const { t } = useTranslation();
  const { lyrics, lyricsFormat, position, track, lyricsOffset, adjustLyricsOffset } = usePlayer();
  const [settings, setSettings] = useState<LyricsSettings>(loadLyricsSettings);
  const [showSettings, setShowSettings] = useState(false);
  const [pos, setPos] = useState(loadPos);
  const [size, setSize] = useState<number>(loadSize);
  const posRef = useRef(pos);
  const sizeRef = useRef(size);
  const dragRef = useRef<{ startX: number; startY: number; origX: number; origY: number } | null>(
    null,
  );
  const resizeRef = useRef<{
    dir: ResizeDir;
    startX: number;
    startY: number;
    origW: number;
  } | null>(null);

  const updateSettings = (patch: Partial<LyricsSettings>) => {
    setSettings((s) => {
      const next = { ...s, ...patch };
      saveLyricsSettings(next);
      return next;
    });
  };

  const lines = useMemo(() => {
    if (lyricsFormat === "lrc") return parseLRC(lyrics);
    return [];
  }, [lyrics, lyricsFormat]);

  const currentIdx = useMemo(() => {
    if (lines.length === 0) return -1;
    return findCurrentLine(lines, position + lyricsOffset);
  }, [position, lyricsOffset, lines]);

  // Two fixed slots (left/right). The current line sits on the left when its
  // index is even, on the right when odd — so the highlighted line alternates
  // sides, and the other slot always previews the next line.
  const current = currentIdx >= 0 ? lines[currentIdx] : null;
  const next = currentIdx >= 0 && currentIdx + 1 < lines.length ? lines[currentIdx + 1] : null;
  const currentOnLeft = currentIdx < 0 || currentIdx % 2 === 0;
  const leftLine = currentOnLeft ? current : next;
  const rightLine = currentOnLeft ? next : current;

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose]);

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest("button")) return;
    if ((e.target as HTMLElement).closest("[data-resize-handle]")) return;
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      origX: posRef.current.x,
      origY: posRef.current.y,
    };
    e.preventDefault();
  }, []);

  const onResizeStart = useCallback(
    (dir: ResizeDir) => (e: React.MouseEvent) => {
      e.stopPropagation();
      e.preventDefault();
      resizeRef.current = { dir, startX: e.clientX, startY: e.clientY, origW: sizeRef.current };
    },
    [],
  );

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      const r = resizeRef.current;
      if (r) {
        const w = Math.max(MIN_W, r.origW + (e.clientX - r.startX));
        sizeRef.current = w;
        setSize(w);
        return;
      }

      const d = dragRef.current;
      if (!d) return;
      const nextPos = {
        x: d.origX + (e.clientX - d.startX),
        y: d.origY + (e.clientY - d.startY),
      };
      posRef.current = nextPos;
      setPos(nextPos);
    };

    const onUp = () => {
      if (resizeRef.current) {
        resizeRef.current = null;
        try {
          localStorage.setItem(SIZE_KEY, JSON.stringify({ w: sizeRef.current }));
        } catch {}
      }
      if (dragRef.current) {
        dragRef.current = null;
        try {
          localStorage.setItem(POS_KEY, JSON.stringify(posRef.current));
        } catch {}
      }
    };

    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
    return () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
  }, []);

  const bgColor = `rgba(24, 24, 27, ${settings.opacity / 100})`;

  return (
    <div
      className="group fixed z-50 select-none cursor-move px-4 py-3 rounded-lg shadow-2xl"
      style={{
        left: pos.x,
        top: pos.y,
        width: size,
        backgroundColor: bgColor,
      }}
      onMouseDown={onMouseDown}
    >
      {/* hover toolbar */}
      <div
        className="absolute top-1 right-2 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity duration-200 z-20"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <button
          onClick={() => updateSettings({ fontSize: Math.max(16, settings.fontSize - 4) })}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer"
          title="Smaller"
        >
          <span className="text-sm font-bold">A-</span>
        </button>
        <button
          onClick={() => updateSettings({ fontSize: Math.min(60, settings.fontSize + 4) })}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer"
          title="Larger"
        >
          <span className="text-base font-bold">A+</span>
        </button>
        <button
          onClick={() => adjustLyricsOffset(-0.1)}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer"
          title="Delay -0.1s"
          aria-label="Delay lyrics by 0.1s"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        <span className="text-xs text-zinc-500 tabular-nums w-10 text-center">
          {lyricsOffset >= 0 ? "+" : ""}
          {lyricsOffset.toFixed(1)}s
        </span>
        <button
          onClick={() => adjustLyricsOffset(0.1)}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer"
          title="Ahead +0.1s"
          aria-label="Advance lyrics by 0.1s"
        >
          <ChevronRight className="w-4 h-4" />
        </button>
        <button
          onClick={() => setShowSettings(!showSettings)}
          className={`p-1 rounded cursor-pointer ${showSettings ? "text-green-400" : "text-zinc-400 hover:text-white hover:bg-zinc-800/80"}`}
          title="Settings"
        >
          <Settings className="w-4 h-4" />
        </button>
        <button
          onClick={onClose}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer"
          title="Close"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {showSettings && (
        <div
          className="absolute right-2 top-9 w-56 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl p-4 space-y-3 z-30"
          onMouseDown={(e) => e.stopPropagation()}
        >
          <div>
            <div className="text-xs text-zinc-400 mb-1.5">背景透明度：{settings.opacity}%</div>
            <input
              type="range"
              min="0"
              max="100"
              value={settings.opacity}
              onChange={(e) => updateSettings({ opacity: parseInt(e.target.value, 10) })}
              className="w-full accent-green-500 cursor-pointer"
            />
          </div>
          <div>
            <div className="text-xs text-zinc-400 mb-1.5">歌词颜色</div>
            <div className="flex gap-2 flex-wrap">
              {COLOR_PRESETS.map((c) => (
                <button
                  key={c.value}
                  onClick={() => updateSettings({ activeColor: c.value })}
                  className={`w-7 h-7 rounded-full cursor-pointer border transition-transform hover:scale-110 ${
                    settings.activeColor === c.value
                      ? "border-white ring-2 ring-white/30"
                      : "border-transparent"
                  }`}
                  style={{ backgroundColor: c.value }}
                  title={c.name}
                />
              ))}
            </div>
          </div>
        </div>
      )}

      {/* lyrics content */}
      <div>
        {lyricsFormat === "lrc" && lines.length > 0 ? (
          <div className="flex flex-col gap-1 pt-5">
            <div
              className="leading-tight break-words"
              style={{
                fontSize: settings.fontSize,
                textAlign: "left",
                color: currentOnLeft ? settings.activeColor : DIM_COLOR,
                fontWeight: currentOnLeft ? 600 : 400,
              }}
            >
              {leftLine ? leftLine.text : "\u00A0"}
            </div>
            <div
              className="leading-tight break-words"
              style={{
                fontSize: settings.fontSize,
                textAlign: "right",
                color: currentOnLeft ? DIM_COLOR : settings.activeColor,
                fontWeight: currentOnLeft ? 400 : 600,
              }}
            >
              {rightLine ? rightLine.text : "\u00A0"}
            </div>
          </div>
        ) : lyrics ? (
          <div
            className="leading-relaxed break-words pt-5 overflow-y-auto"
            style={{ fontSize: settings.fontSize, color: settings.activeColor, maxHeight: 320 }}
          >
            {lyrics}
          </div>
        ) : (
          <div
            className="text-zinc-600 italic pt-5"
            style={{ fontSize: Math.round(settings.fontSize * 0.6) }}
          >
            {track ? t("player.noLyrics") : t("player.notPlaying")}
          </div>
        )}
      </div>

      {/* resize handle: right edge only */}
      <div
        data-resize-handle
        className="absolute right-0 top-0 bottom-0 w-1.5 cursor-ew-resize opacity-0 group-hover:opacity-100"
        onMouseDown={onResizeStart("e")}
      />
    </div>
  );
}
