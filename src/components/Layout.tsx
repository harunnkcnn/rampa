import type { ReactNode } from "react";
import { formatToday } from "../lib/format";

export type Tab = "stock" | "movement" | "shipment" | "settings";

export function Header() {
  return (
    <header className="sticky top-0 z-10 bg-asphalt text-white">
      <div aria-hidden className="hazard-stripe h-1.5" />
      <div className="mx-auto flex max-w-3xl items-center justify-between gap-3 px-4 py-3">
        <h1 className="flex items-center gap-2.5 text-[26px] leading-none tracking-[0.08em]">
          <span aria-hidden className="inline-block h-[22px] w-3.5 rounded-sm bg-safety" />
          Rampa
        </h1>
        <small className="truncate text-sm text-white/70">{formatToday()}</small>
      </div>
    </header>
  );
}

const ICONS: Record<Tab, ReactNode> = {
  stock: (
    <>
      <path d="M3 7l9-4 9 4-9 4-9-4z" />
      <path d="M3 7v10l9 4 9-4V7" />
      <path d="M12 11v10" />
    </>
  ),
  movement: (
    <>
      <path d="M7 4v16M3 8l4-4 4 4" />
      <path d="M17 20V4M13 16l4 4 4-4" />
    </>
  ),
  shipment: (
    <>
      <path d="M1 6h13v10H1z" />
      <path d="M14 10h4l4 4v2h-8z" />
      <circle cx="6" cy="18" r="2" />
      <circle cx="18" cy="18" r="2" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 2v3M12 19v3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M2 12h3M19 12h3M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1" />
    </>
  ),
};

const TABS: { id: Tab; label: string }[] = [
  { id: "stock", label: "Stok" },
  { id: "movement", label: "Giriş/Çıkış" },
  { id: "shipment", label: "Sevkiyat" },
  { id: "settings", label: "Ayarlar" },
];

export function TabBar({ active, onChange }: { active: Tab; onChange: (tab: Tab) => void }) {
  return (
    <nav
      aria-label="Bölümler"
      className="fixed inset-x-0 bottom-0 z-10 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)]"
    >
      <div className="mx-auto grid max-w-3xl grid-cols-4">
        {TABS.map((tab) => {
          const current = tab.id === active;
          return (
            <button
              key={tab.id}
              type="button"
              aria-current={current ? "page" : undefined}
              onClick={() => onChange(tab.id)}
              className={`relative grid min-h-16 cursor-pointer place-items-center gap-0.5 text-[13px] font-semibold ${current ? "text-ink" : "text-muted"}`}
            >
              {current && <span aria-hidden className="absolute inset-x-[22%] top-0 h-1 rounded-b bg-safety" />}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} aria-hidden className="size-6">
                {ICONS[tab.id]}
              </svg>
              {tab.label}
            </button>
          );
        })}
      </div>
    </nav>
  );
}
