import { useState, useRef, useEffect } from "react";
import { Globe } from "lucide-react";

const LANGUAGES = [
  { code: "en", label: "English" },
  { code: "es", label: "Español" },
  { code: "fr", label: "Français" },
  { code: "de", label: "Deutsch" },
  { code: "zh", label: "中文" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
];

export function LanguageToggle() {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState("en");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center justify-center h-8 w-8 rounded-md transition-colors"
        style={{ color: "var(--fg-muted)" }}
        aria-label="Language"
      >
        <Globe className="w-4 h-4" />
      </button>
      {open && (
        <div
          className="absolute right-0 top-full mt-1 z-50 py-1 rounded-lg shadow-lg overflow-hidden"
          style={{
            background: "var(--bg-elevated)",
            border: "1px solid var(--border-soft)",
            minWidth: 140,
          }}
        >
          {LANGUAGES.map((lang) => (
            <button
              key={lang.code}
              onClick={() => { setSelected(lang.code); setOpen(false); }}
              className="w-full text-left px-3 py-2 text-xs transition-colors"
              style={{
                color: selected === lang.code ? "var(--accent-solid)" : "var(--fg)",
                background: selected === lang.code ? "rgba(0, 76, 237, 0.08)" : "transparent",
                fontWeight: selected === lang.code ? 600 : 400,
              }}
            >
              {lang.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
