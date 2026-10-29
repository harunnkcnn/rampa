import { useState } from "react";
import { Header, TabBar, type Tab } from "./components/Layout";
import { MovementView } from "./components/MovementView";
import { SettingsView } from "./components/SettingsView";
import { ShipmentView } from "./components/ShipmentView";
import { StockView } from "./components/StockView";
import { ToastProvider } from "./components/Toast";
import type { MovementType } from "./domain/types";
import { useRampa } from "./hooks/useRampa";

const TAB_HASH: Record<Tab, string> = { stock: "stok", movement: "hareket", shipment: "sevkiyat", settings: "ayarlar" };

function initialTab(): Tab {
  const hash = window.location.hash.slice(1);
  const match = (Object.keys(TAB_HASH) as Tab[]).find((t) => TAB_HASH[t] === hash);
  return match ?? "stock";
}

export default function App() {
  const { data, apply, replace, storageOk } = useRampa();
  const [tab, setTab] = useState<Tab>(initialTab);
  const [preset, setPreset] = useState<{ type: MovementType; partId: string } | null>(null);

  function go(next: Tab) {
    setTab(next);
    if (next !== "movement") setPreset(null);
    window.history.replaceState(null, "", `#${TAB_HASH[next]}`);
    window.scrollTo(0, 0);
  }

  return (
    <ToastProvider>
      <Header />
      <main className="mx-auto max-w-3xl px-4">
        {tab === "stock" && (
          <StockView
            data={data}
            apply={apply}
            onQuickMove={(type, partId) => {
              go("movement");
              setPreset({ type, partId });
            }}
          />
        )}
        {tab === "movement" && <MovementView data={data} apply={apply} preset={preset} />}
        {tab === "shipment" && <ShipmentView data={data} apply={apply} />}
        {tab === "settings" && <SettingsView data={data} replace={replace} storageOk={storageOk} />}
      </main>
      <TabBar active={tab} onChange={go} />
    </ToastProvider>
  );
}
