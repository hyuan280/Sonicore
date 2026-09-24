import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check } from "lucide-react";
import { cn } from "../../lib/utils";

export interface TriggerAriaProps {
  "aria-haspopup": "menu";
  "aria-expanded": boolean;
}

interface DropdownMenuProps {
  trigger: (toggle: () => void, open: boolean, ariaProps: TriggerAriaProps) => React.ReactNode;
  align?: "left" | "right";
  className?: string;
  children: React.ReactNode;
}

export const MenuContext = createContext<{ close: () => void }>({ close: () => {} });

// handleMenuKeyDown implements WAI-ARIA arrow-key navigation over the
// menuitem elements inside a menu panel. Used by DropdownMenu.
export function handleMenuKeyDown(panel: HTMLElement | null, e: React.KeyboardEvent) {
  const items = Array.from(panel?.querySelectorAll<HTMLElement>('button[role^="menuitem"]') || []);
  if (items.length === 0) return;
  const idx = items.indexOf(document.activeElement as HTMLElement);
  if (e.key === "ArrowDown") {
    e.preventDefault();
    items[(idx + 1) % items.length].focus();
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    items[(idx - 1 + items.length) % items.length].focus();
  } else if (e.key === "Home") {
    e.preventDefault();
    items[0].focus();
  } else if (e.key === "End") {
    e.preventDefault();
    items[items.length - 1].focus();
  }
}

// The menu panel is rendered through a portal with fixed positioning so it
// can escape clipped/scrollable ancestors (e.g. the plugins tab row with
// overflow-x-auto) instead of being cut off inside them.
export function DropdownMenu({ trigger, align = "right", className, children }: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left?: number; right?: number } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const updatePos = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const next: { top: number; left?: number; right?: number } = { top: rect.bottom + 4 };
    if (align === "right") {
      next.right = Math.max(4, window.innerWidth - rect.right);
    } else {
      next.left = Math.max(4, rect.left);
    }
    setPos(next);
  }, [align]);

  const toggle = useCallback(() => {
    setOpen((prev) => !prev);
  }, []);

  useEffect(() => {
    if (!open) return;
    updatePos();
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (containerRef.current?.contains(target) || panelRef.current?.contains(target)) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    // Keep the panel aligned while any scrollable ancestor or the window
    // scrolls/resizes.
    window.addEventListener("resize", updatePos);
    window.addEventListener("scroll", updatePos, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", updatePos);
      window.removeEventListener("scroll", updatePos, true);
    };
  }, [open, updatePos]);

  return (
    <div className="relative" ref={containerRef}>
      {trigger(toggle, open, { "aria-haspopup": "menu", "aria-expanded": open })}
      {open &&
        pos &&
        createPortal(
          <div
            ref={panelRef}
            role="menu"
            onKeyDown={(e) => handleMenuKeyDown(panelRef.current, e)}
            // The panel is portaled, but React events still bubble through
            // the React tree — stop them here too so clicks on the panel's
            // padding/background never reach a clickable parent (e.g. a
            // plugin card's onClick).
            onClick={(e) => e.stopPropagation()}
            style={pos}
            className={cn(
              "fixed z-50 bg-zinc-800 rounded-lg shadow-xl py-1 border border-zinc-700/50 min-w-44",
              className,
            )}
          >
            <MenuContext.Provider value={{ close: () => setOpen(false) }}>
              {children}
            </MenuContext.Provider>
          </div>,
          document.body,
        )}
    </div>
  );
}

export function MenuItem({
  onClick,
  icon,
  danger,
  disabled,
  onMouseEnter,
  children,
}: {
  onClick?: () => boolean | void | Promise<boolean | void>;
  icon?: React.ReactNode;
  danger?: boolean;
  disabled?: boolean;
  onMouseEnter?: React.MouseEventHandler<HTMLButtonElement>;
  children: React.ReactNode;
}) {
  const { close } = useContext(MenuContext);
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onMouseEnter={onMouseEnter}
      onClick={async (e) => {
        // The panel is portaled, but React events still bubble through the
        // React tree — stop them so a menu inside a clickable card doesn't
        // also trigger the card's onClick.
        e.stopPropagation();
        // A handler returning false (or resolving to false) keeps the menu
        // open, e.g. to show an inline error after a failed action.
        let result: boolean | void;
        try {
          result = await onClick?.();
        } catch (err) {
          console.warn("[menu] menu item action failed", err);
          result = undefined;
        }
        if (result !== false) close();
      }}
      className={cn(
        "w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed",
        danger ? "text-red-400 hover:text-red-300" : "text-zinc-300 hover:text-white",
        "hover:bg-zinc-700/60",
      )}
    >
      {icon && <span className="shrink-0">{icon}</span>}
      <span className="flex-1 truncate">{children}</span>
    </button>
  );
}

export function MenuDivider() {
  return <div className="border-t border-zinc-700/50 my-1" />;
}

export function MenuLabel({ children }: { children: React.ReactNode }) {
  return <div className="px-3 py-1 text-xs text-zinc-500">{children}</div>;
}

export function MenuCheckbox({
  label,
  checked,
  onToggle,
}: {
  label: string;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitemcheckbox"
      aria-checked={checked}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      className="w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 text-zinc-300 hover:text-white hover:bg-zinc-700/60"
    >
      <span
        className={cn(
          "w-4 h-4 rounded border flex items-center justify-center shrink-0",
          checked ? "bg-green-600 border-green-600" : "border-zinc-600",
        )}
      >
        {checked && <Check className="w-3 h-3 text-white" />}
      </span>
      <span className="flex-1 truncate">{label}</span>
    </button>
  );
}

export function MenuRadio({
  label,
  checked,
  onSelect,
}: {
  label: string;
  checked: boolean;
  onSelect: () => void;
}) {
  const { close } = useContext(MenuContext);
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      onClick={(e) => {
        e.stopPropagation();
        onSelect();
        close();
      }}
      className="w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 text-zinc-300 hover:text-white hover:bg-zinc-700/60"
    >
      <span className="w-4 h-4 shrink-0 flex items-center justify-center">
        {checked && <Check className="w-3.5 h-3.5 text-green-500" />}
      </span>
      <span className="flex-1 truncate">{label}</span>
    </button>
  );
}
