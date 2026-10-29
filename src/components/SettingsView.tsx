import type { ChangeEvent } from "react";
import { demoData } from "../domain/demo";
import { emptyData, parseBackup } from "../domain/rampa";
import type { RampaData } from "../domain/types";
import { downloadBackup } from "../lib/storage";
import { Button, Card } from "./ui";
import { useToast } from "./Toast";

interface Props {
  data: RampaData;
  replace: (next: RampaData) => void;
  storageOk: boolean;
}

export function SettingsView({ data, replace, storageOk }: Props) {
  const toast = useToast();

  async function restore(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    const result = parseBackup(await file.text());
    if (!result.ok) return toast(result.error);
    replace(result.value);
    toast("Yedek yüklendi");
  }

  function reset(next: RampaData, message: string, done: string) {
    if (!window.confirm(message)) return;
    replace(next);
    toast(done);
  }

  return (
    <section className="grid gap-2">
      <h2 className="mb-1 mt-5 text-2xl">Ayarlar</h2>

      <Card>
        <h3 className="text-lg">Yedek</h3>
        <p className="mt-1 text-muted">
          Veriler yalnızca bu cihazdaki tarayıcıda saklanır. Düzenli yedek almanızı öneririz.
          {!storageOk && <strong className="mt-1 block text-crit">Tarayıcı depolaması kapalı: değişiklikler kaydedilmiyor.</strong>}
        </p>
        <div className="mt-2.5 flex flex-wrap gap-2">
          <Button className="flex-1" onClick={() => downloadBackup(data)}>
            Yedek indir
          </Button>
          <label className="inline-flex min-h-12 flex-1 cursor-pointer items-center justify-center rounded-xl border border-line bg-surface px-4 font-semibold">
            Yedekten yükle
            <input type="file" accept="application/json" className="hidden" onChange={restore} />
          </label>
        </div>
      </Card>

      <Card>
        <h3 className="text-lg">Demo verisi</h3>
        <p className="mt-1 text-muted">Tüm kayıtları silip örnek verilerle ya da boş olarak yeniden başlatır.</p>
        <div className="mt-2.5 flex flex-wrap gap-2">
          <Button
            className="flex-1"
            onClick={() => reset(demoData(), "Tüm kayıtlar silinip demo verisi yüklenecek. Devam edilsin mi?", "Demo verisi yüklendi")}
          >
            Demo verisine dön
          </Button>
          <Button
            className="flex-1"
            onClick={() => reset(emptyData(), "Tüm kayıtlar silinecek. Devam edilsin mi?", "Boş başlatıldı")}
          >
            Boş başla
          </Button>
        </div>
      </Card>

      <Card className="border-l-[6px] border-l-safety">
        <h3 className="text-lg">Rampa Pro</h3>
        <p className="mt-1 text-muted">
          Bu açık kaynak sürümde stok, giriş/çıkış ve sevkiyat takibi var. Pro sürümde 7 günlük rapor, kritik stok
          sipariş önerisi ve araç doluluk planı bulunuyor.
        </p>
      </Card>

      <footer className="mb-3 mt-7 text-center text-sm text-muted">
        Rampa · Açık kaynak (MIT) ·{" "}
        <a href="https://github.com/harunnkcnn/rampa" target="_blank" rel="noopener" className="underline">
          GitHub
        </a>
      </footer>
    </section>
  );
}
