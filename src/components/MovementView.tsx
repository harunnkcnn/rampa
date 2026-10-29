import { useEffect, useMemo, useState, type FormEvent } from "react";
import { recordMovement } from "../domain/rampa";
import type { MovementType, RampaData, Result } from "../domain/types";
import { cx, formatDateTime } from "../lib/format";
import { Button, Card, EmptyState, ErrorText, Field, Segmented } from "./ui";
import { useToast } from "./Toast";

interface Props {
  data: RampaData;
  apply: (op: (d: RampaData) => Result<RampaData>) => string | null;
  preset: { type: MovementType; partId: string } | null;
}

export function MovementView({ data, apply, preset }: Props) {
  const toast = useToast();
  const [type, setType] = useState<MovementType>(preset?.type ?? "in");
  const [partId, setPartId] = useState(preset?.partId ?? data.parts[0]?.id ?? "");
  const [qty, setQty] = useState("");
  const [doc, setDoc] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (preset) {
      setType(preset.type);
      setPartId(preset.partId);
    }
  }, [preset]);

  const sortedParts = useMemo(() => [...data.parts].sort((a, b) => a.name.localeCompare(b.name, "tr")), [data.parts]);
  const recent = useMemo(() => [...data.movements].sort((a, b) => b.ts - a.ts).slice(0, 50), [data.movements]);

  function submit(e: FormEvent) {
    e.preventDefault();
    const err = apply((d) => recordMovement(d, { type, partId, qty: qty.trim() === "" ? Number.NaN : Number(qty), doc, note }));
    if (err) return setError(err);
    setError("");
    setQty("");
    setDoc("");
    setNote("");
    toast(type === "in" ? "Giriş kaydedildi" : "Çıkış kaydedildi");
  }

  return (
    <section>
      <h2 className="mb-3 mt-5 text-2xl">Giriş / Çıkış</h2>
      <form onSubmit={submit} noValidate>
        <Card className="grid gap-3">
          <Segmented
            label="Hareket türü"
            value={type}
            onChange={setType}
            options={[
              { value: "in", label: "Giriş" },
              { value: "out", label: "Çıkış" },
            ]}
          />
          <Field label="Parça">
            <select value={partId} onChange={(e) => setPartId(e.target.value)}>
              {sortedParts.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.oem} · stok {p.qty}
                </option>
              ))}
            </select>
          </Field>
          <div className="grid gap-2.5 sm:grid-cols-2">
            <Field label="Adet">
              <input type="number" inputMode="numeric" min={1} step={1} value={qty} onChange={(e) => setQty(e.target.value)} />
            </Field>
            <Field label="Belge no" hint="İrsaliye / iş emri">
              <input className="font-mono" placeholder="IRS-000000" autoComplete="off" value={doc} onChange={(e) => setDoc(e.target.value)} />
            </Field>
          </div>
          <Field label="Not" hint="isteğe bağlı">
            <input autoComplete="off" value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
          <ErrorText>{error}</ErrorText>
          <Button type="submit" variant="primary" className="w-full text-lg" disabled={!partId}>
            Kaydet
          </Button>
        </Card>
      </form>

      <h2 className="mb-3 mt-5 text-2xl">Son hareketler</h2>
      <div className="mb-3 flex flex-col gap-2">
        {recent.length === 0 ? (
          <EmptyState>Henüz hareket yok.</EmptyState>
        ) : (
          recent.map((m) => {
            const part = data.parts.find((p) => p.id === m.partId);
            return (
              <Card key={m.id} className="grid grid-cols-[auto_1fr_auto] items-center gap-2.5">
                <div
                  aria-label={m.type === "in" ? "Giriş" : "Çıkış"}
                  className={cx(
                    "grid size-9 place-items-center rounded-lg font-head text-2xl",
                    m.type === "in" ? "bg-ok-soft text-ok" : "bg-crit-soft text-crit",
                  )}
                >
                  {m.type === "in" ? "+" : "−"}
                </div>
                <div>
                  <b>{part ? part.name : "Silinmiş parça"}</b>
                  <small className="block text-sm text-muted">
                    <span className="font-mono">{m.doc}</span> · {formatDateTime(m.ts)}
                    {m.note && ` · ${m.note}`}
                  </small>
                </div>
                <div className="font-head text-2xl">{m.qty}</div>
              </Card>
            );
          })
        )}
      </div>
    </section>
  );
}
