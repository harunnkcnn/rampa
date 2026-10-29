import { useCallback, useEffect, useRef, useState } from "react";
import { demoData } from "../domain/demo";
import type { RampaData, Result } from "../domain/types";
import { loadData, saveData } from "../lib/storage";

/**
 * Uygulama verisini tutar ve her değişiklikte tarayıcıya kaydeder.
 * `apply` bir domain işlemini çalıştırır; başarısızsa hata mesajını, başarılıysa null döner.
 */
export function useRampa() {
  const [data, setData] = useState<RampaData>(() => loadData() ?? demoData());
  const latest = useRef(data);
  const [storageOk, setStorageOk] = useState(true);

  useEffect(() => {
    setStorageOk(saveData(data));
  }, [data]);

  const replace = useCallback((next: RampaData) => {
    latest.current = next;
    setData(next);
  }, []);

  const apply = useCallback(
    (operation: (current: RampaData) => Result<RampaData>): string | null => {
      const result = operation(latest.current);
      if (!result.ok) return result.error;
      replace(result.value);
      return null;
    },
    [replace],
  );

  return { data, apply, replace, storageOk };
}
