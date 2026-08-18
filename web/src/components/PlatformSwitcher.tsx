import { useEffect, useRef } from "react";

export interface PlatformItem {
  name: string;
  label: string;
}

interface PlatformSwitcherProps {
  platforms: PlatformItem[];
  platform: string;
  onChange: (name: string) => void;
}

export default function PlatformSwitcher({ platforms, platform, onChange }: PlatformSwitcherProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef({ dragging: false, moved: false, startX: 0, startLeft: 0 });

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) return;
      if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
        const maxLeft = el.scrollWidth - el.clientWidth;
        const canScroll =
          (e.deltaY > 0 && el.scrollLeft < maxLeft) || (e.deltaY < 0 && el.scrollLeft > 0);
        if (!canScroll) return;
        el.scrollLeft += e.deltaY;
        e.preventDefault();
      }
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const onMouseDown = (e: React.MouseEvent) => {
    const el = listRef.current;
    if (!el) return;
    dragRef.current = { dragging: true, moved: false, startX: e.pageX, startLeft: el.scrollLeft };
  };

  const onMouseMove = (e: React.MouseEvent) => {
    const d = dragRef.current;
    const el = listRef.current;
    if (!d.dragging || !el) return;
    const dx = e.pageX - d.startX;
    if (Math.abs(dx) > 4) d.moved = true;
    el.scrollLeft = d.startLeft - dx;
  };

  const stopDrag = () => {
    dragRef.current.dragging = false;
  };

  const onClickCapture = (e: React.MouseEvent) => {
    if (dragRef.current.moved) {
      e.preventDefault();
      e.stopPropagation();
      dragRef.current.moved = false;
    }
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const btns = Array.from(
      listRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]') || [],
    );
    if (btns.length === 0) return;
    const current = Math.max(
      0,
      btns.findIndex((b) => b.getAttribute("aria-selected") === "true"),
    );
    const next =
      e.key === "ArrowRight"
        ? (current + 1) % btns.length
        : (current - 1 + btns.length) % btns.length;
    btns[next].focus();
    onChange(platforms[next].name);
  };

  return (
    <div
      ref={listRef}
      onMouseDown={onMouseDown}
      onMouseMove={onMouseMove}
      onMouseUp={stopDrag}
      onMouseLeave={stopDrag}
      onClickCapture={onClickCapture}
      onKeyDown={onKeyDown}
      role="tablist"
      className="no-scrollbar flex items-center gap-[2px] overflow-x-auto pb-1 cursor-grab active:cursor-grabbing select-none"
    >
      {platforms.map((p) => (
        <button
          key={p.name}
          type="button"
          role="tab"
          aria-selected={platform === p.name}
          tabIndex={platform === p.name ? 0 : -1}
          onClick={() => onChange(p.name)}
          className={`px-4 py-1.5 text-sm transition-colors cursor-pointer whitespace-nowrap shrink-0 clip-plat ${
            platform === p.name
              ? "bg-green-600/25 text-green-500"
              : "bg-zinc-800 text-zinc-400 hover:text-white"
          }`}
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}
