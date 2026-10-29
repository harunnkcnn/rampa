import { parseBackup } from "../domain/rampa";
import type { RampaData } from "../domain/types";

const KEY = "rampa.v1";

/** Tarayıcıda kayıtlı veriyi okur; yoksa veya okunamazsa null döner */
export function loadData(): RampaData | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const parsed = parseBackup(raw);
    return parsed.ok ? parsed.value : null;
  } catch {
    return null;
  }
}

/** Kaydedebildiyse true döner (gizli pencere veya kapalı depolamada false) */
export function saveData(data: RampaData): boolean {
  try {
    localStorage.setItem(KEY, JSON.stringify(data));
    return true;
  } catch {
    return false;
  }
}

export function downloadBackup(data: RampaData): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `rampa-yedek-${new Date().toISOString().slice(0, 10)}.json`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
