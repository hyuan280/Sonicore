import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Globe, ChevronDown } from "lucide-react";
import { switchLanguage } from "../i18n";

const LANGUAGES = { en: "English", zh: "中文" } as const;

export default function LanguageSwitcher() {
  const { i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const current = i18n.language?.split("-")[0] as keyof typeof LANGUAGES | undefined;

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer transition-colors"
      >
        <Globe className="w-4 h-4" />
        {current ? LANGUAGES[current] : "English"}
        <ChevronDown className={`w-3.5 h-3.5 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-50 py-1 min-w-[130px]">
          {(Object.keys(LANGUAGES) as (keyof typeof LANGUAGES)[]).map((lang) => (
            <button
              key={lang}
              onClick={async () => {
                try {
                  await switchLanguage(lang);
                } catch (e) {
                  console.error("Failed to switch language", e);
                }
                setOpen(false);
              }}
              className={`w-full text-left px-4 py-2 text-sm cursor-pointer transition-colors ${
                current === lang ? "text-green-500" : "text-zinc-400 hover:text-white"
              }`}
            >
              {LANGUAGES[lang]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
