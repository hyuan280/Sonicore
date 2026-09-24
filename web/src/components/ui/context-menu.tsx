import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronRight } from "lucide-react";
import { cn } from "../../lib/utils";
import { MenuContext, MenuItem } from "./menu";

// ContextMenu renders a pointer-driven right-click menu through a portal with
// fixed positioning. The panel is clamped to the viewport and closes on
// outside mousedown, Escape, scroll or resize. Keyboard-only operation is not
// supported (the menu can only be triggered by a contextmenu event), so there
// is deliberately no focus handling. Items may expose a hover/click submenu
// that flips to the left/up when there is no room on the right/below.

const VIEWPORT_MARGIN = 8;
const SUBMENU_PANEL_WIDTH = 208; // matches the panel below
const SUBMENU_GAP = 4; // pl-1 / pr-1 of the submenu wrapper

const SubmenuAlignContext = createContext<"left" | "right">("right");

export function ContextMenu({
  x,
  y,
  onClose,
  error,
  children,
}: {
  x: number;
  y: number;
  onClose: () => void;
  error?: string;
  children: React.ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  const [offset, setOffset] = useState({ dx: 0, dy: 0 });
  const [submenuAlign, setSubmenuAlign] = useState<"left" | "right">("right");
  const [errorAbove, setErrorAbove] = useState(false);

  useLayoutEffect(() => {
    const el = panelRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    let dx = 0;
    let dy = 0;
    if (x + rect.width + VIEWPORT_MARGIN > window.innerWidth) {
      dx = Math.max(VIEWPORT_MARGIN, window.innerWidth - rect.width - VIEWPORT_MARGIN) - x;
    }
    if (y + rect.height + VIEWPORT_MARGIN > window.innerHeight) {
      dy = Math.max(VIEWPORT_MARGIN, window.innerHeight - rect.height - VIEWPORT_MARGIN) - y;
    }
    setOffset({ dx, dy });
    // The submenu opens to the right unless the (clamped) panel plus the
    // submenu and its gap would overflow the viewport.
    const needed = rect.width + SUBMENU_PANEL_WIDTH + SUBMENU_GAP;
    setSubmenuAlign(x + dx + needed > window.innerWidth ? "left" : "right");
  }, [x, y]);

  // The error bubble is absolutely positioned so it never grows the panel (the
  // items must not shift under the cursor). Flip it above the panel when there
  // is no room below.
  useLayoutEffect(() => {
    if (!error) return;
    const panel = panelRef.current;
    const el = errorRef.current;
    if (!panel || !el) return;
    const panelRect = panel.getBoundingClientRect();
    const h = el.getBoundingClientRect().height;
    setErrorAbove(panelRect.bottom + 4 + h + VIEWPORT_MARGIN > window.innerHeight);
  }, [error]);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (panelRef.current?.contains(e.target as Node)) return;
      onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    // Ignore scrolls that happen inside the panel (e.g. the playlist submenu
    // list); only viewport/ancestor scrolls should dismiss the menu.
    const onViewport = (e: Event) => {
      if (panelRef.current?.contains(e.target as Node)) return;
      onClose();
    };
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onViewport, true);
    window.addEventListener("resize", onViewport);
    return () => {
      document.removeEventListener("mousedown", onDown, true);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onViewport, true);
      window.removeEventListener("resize", onViewport);
    };
  }, [onClose]);

  return createPortal(
    <div
      ref={panelRef}
      role="menu"
      style={{ top: y + offset.dy, left: x + offset.dx }}
      onContextMenu={(e) => e.preventDefault()}
      className="fixed z-[70] min-w-44 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl py-1"
    >
      <MenuContext.Provider value={{ close: onClose }}>
        <SubmenuAlignContext.Provider value={submenuAlign}>{children}</SubmenuAlignContext.Provider>
      </MenuContext.Provider>
      {error && (
        <div
          ref={errorRef}
          role="status"
          className={cn(
            "absolute left-0 right-0 px-3 py-2 text-xs text-red-400 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl",
            errorAbove ? "bottom-full mb-1" : "top-full mt-1",
          )}
        >
          {error}
        </div>
      )}
    </div>,
    document.body,
  );
}

// ContextMenuItem renders a simple MenuItem, or a submenu-opening item when
// `submenu` is given. The submenu opens on hover or click (so it is reachable
// with touch as well) and closes when the pointer leaves. It flips left/up
// when it would overflow the viewport.
export function ContextMenuItem({
  icon,
  onClick,
  danger,
  disabled,
  submenu,
  onHover,
  children,
}: {
  icon?: React.ReactNode;
  onClick?: () => boolean | void | Promise<boolean | void>;
  danger?: boolean;
  disabled?: boolean;
  submenu?: React.ReactNode;
  // Called when the pointer moves onto this item (a real focus switch).
  onHover?: () => void;
  children: React.ReactNode;
}) {
  const submenuAlign = useContext(SubmenuAlignContext);
  const [subOpen, setSubOpen] = useState(false);
  const [flipY, setFlipY] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (!submenu || !subOpen) return;
    const el = panelRef.current;
    // Measure against the anchor (the item), whose top is independent of
    // whether the submenu is currently flipped; measuring the panel itself
    // would make the result depend on the current flip and oscillate.
    const anchor = el?.parentElement;
    if (!el || !anchor) return;
    const a = anchor.getBoundingClientRect();
    const h = el.getBoundingClientRect().height;
    setFlipY(a.top + h + VIEWPORT_MARGIN > window.innerHeight);
  }, [submenu, subOpen]);

  if (!submenu) {
    return (
      <MenuItem
        icon={icon}
        danger={danger}
        disabled={disabled}
        onClick={onClick}
        onMouseEnter={onHover}
      >
        {children}
      </MenuItem>
    );
  }

  return (
    <div
      className="relative"
      onMouseEnter={() => {
        if (disabled) return;
        onHover?.();
        setSubOpen(true);
      }}
      onMouseLeave={() => setSubOpen(false)}
    >
      <button
        type="button"
        role="menuitem"
        aria-haspopup="menu"
        aria-expanded={subOpen}
        disabled={disabled}
        onClick={async (e) => {
          e.stopPropagation();
          try {
            await onClick?.();
          } catch (err) {
            console.warn("[context-menu] menu item action failed", err);
          }
          setSubOpen(true);
        }}
        className={cn(
          "w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed",
          danger ? "text-red-400 hover:text-red-300" : "text-zinc-300 hover:text-white",
          "hover:bg-zinc-700/60",
        )}
      >
        {icon && <span className="shrink-0">{icon}</span>}
        <span className="flex-1 truncate">{children}</span>
        <ChevronRight className="w-3.5 h-3.5 shrink-0 opacity-60" />
      </button>
      {subOpen && (
        <div
          className={cn(
            "absolute",
            submenuAlign === "left" ? "right-full pr-1" : "left-full pl-1",
            flipY ? "bottom-0" : "top-0",
          )}
        >
          <div
            ref={panelRef}
            role="menu"
            style={{ width: SUBMENU_PANEL_WIDTH }}
            className="max-h-64 overflow-y-auto bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl py-1"
          >
            {submenu}
          </div>
        </div>
      )}
    </div>
  );
}
