import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { cn } from "../../lib/utils";

interface ModalProps {
  title?: string;
  onClose: () => void;
  className?: string;
  children: React.ReactNode;
}

export function Modal({ title, onClose, className, children }: ModalProps) {
  const { t } = useTranslation();
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Lock body scroll while the modal is open.
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, []);

  // Initial focus, focus trap and Escape handling. onClose goes through a
  // ref so re-renders of the parent (e.g. a fresh inline onClose prop) never
  // re-run the effect and steal focus.
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;
    const focusables = () =>
      Array.from(
        dialog?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ) || [],
      );
    focusables()[0]?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab") return;
      const els = focusables();
      if (els.length === 0) return;
      const firstEl = els[0];
      const lastEl = els[els.length - 1];
      const outside = !dialog?.contains(document.activeElement);
      if (e.shiftKey && (outside || document.activeElement === firstEl)) {
        e.preventDefault();
        lastEl.focus();
      } else if (!e.shiftKey && (outside || document.activeElement === lastEl)) {
        e.preventDefault();
        firstEl.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      prev?.focus?.();
    };
  }, []);

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCloseRef.current();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        ref={dialogRef}
        className={cn(
          "bg-zinc-900 border border-zinc-800 rounded-xl w-full max-w-lg mx-4 max-h-[85vh] overflow-y-auto",
          className,
        )}
      >
        <div className="flex items-center justify-between px-5 pt-4 pb-2">
          {title ? (
            <h3 id={titleId} className="font-bold text-lg">
              {title}
            </h3>
          ) : (
            <span />
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="px-5 pb-5">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
