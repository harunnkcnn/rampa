import { useMemo, useState, type FormEvent } from "react";
import { addPart, isAlert, searchParts, sortForAttention, stockLevel, updatePart } from "../domain/rampa";
import type { MovementType, Part, RampaData, Result } from "../domain/types";
import { Button, Card, EmptyState, ErrorText, Field, Modal, Segmented, StockPill, stockLabel } from "./ui";
import { useToast } from "./Toast";

type Filter = "all" | "alert";

interface Props {
  data: RampaData;
  apply: (op: (d: RampaData) => Result<RampaData>) => string | null;
  onQuickMove: (type: MovementType, partId: string) => void;
}

export function StockView({ data, apply, onQuickMove }: Props) {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [editing, setEditing] = useState<Part | "new" | null>(null);

  const visible = useMemo(() => {
    const found = searchParts(data.parts, query);
    return sortForAttention(filter === "alert" ? found.filter(isAlert) : found);
  }, [data.parts, query, filter]);

  const alertCount = data.parts.filter(isAlert).length;
  const openShipments = data.shipments.filter((s) => s.status < 3).length;

  return (
    <section>
      <div className="mt-4 grid grid-cols-3 gap-2" aria-live="polite">
        <Stat value={data.parts.length} label="Kalem" />
        <Stat value={alertCount} label="Kritik / tükendi" alert={alertCount > 0} />
        <Stat value={openShipments} label="Açık sevkiyat" />
      </div>

      <h2 className="mb-3 mt-5 text-2xl">Stok</h2>
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Parça kodu, ad veya raf ara"
        aria-label="Stokta ara"
        className="rounded-xl bg-surface"
      />
      <div className="mt-2 flex items-center gap-2">
        <Segmented
          label="Filtre"
          value={filter}
          onChange={setFilter}
          options={[
            { value: "all", label: "Tümü" },
            { value: "alert", label: "Kritik" },
          ]}
        />
        <Button className="ml-auto" onClick={() => setEditing("new")}>
          + Parça
        </Button>
      </div>

      <div className="my-3 flex flex-col gap-2">
        {visible.length === 0 ? (
          <EmptyState>{data.parts.length ? "Aramaya uyan parça yok." : "Henüz parça yok. “+ Parça” ile ekleyin."}</EmptyState>
        ) : (
          visible.map((part) => <PartRow key={part.id} part={part} onOpen={() => setEditing(part)} />)
        )}
      </div>

      <PartDialog
        part={editing}
        onClose={() => setEditing(null)}
        apply={apply}
        onQuickMove={(type, partId) => {
          setEditing(null);
          onQuickMove(type, partId);
        }}
      />
    </section>
  );
}

function Stat({ value, label, alert = false }: { value: number; label: string; alert?: boolean }) {
  return (
    <Card className="px-3 py-2.5">
      <b className={`block font-head text-3xl leading-tight ${alert ? "text-crit" : ""}`}>{value}</b>
      <span className="text-sm text-muted">{label}</span>
    </Card>
  );
}

function PartRow({ part, onOpen }: { part: Part; onOpen: () => void }) {
  const level = stockLevel(part);
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={`${part.name}, ${part.qty} adet, ${stockLabel(level)}`}
      className="grid cursor-pointer grid-cols-[1fr_auto] items-center gap-x-3 gap-y-1 rounded-xl border border-line bg-surface p-3.5 text-left shadow-sm"
    >
      <div>
        <div className="font-mono text-[0.92em]">{part.oem}</div>
        <div className="font-semibold">{part.name}</div>
      </div>
      <div className="row-span-2 text-right">
        <b className="block font-head text-3xl leading-none">{part.qty}</b>
        <small className="text-xs text-muted">adet</small>
      </div>
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-muted">
        <span>
          Raf <span className="font-mono">{part.shelf}</span>
        </span>
        <span>Kritik: {part.min}</span>
        <StockPill level={level} />
      </div>
    </button>
  );
}

function PartDialog({
  part,
  onClose,
  apply,
  onQuickMove,
}: {
  part: Part | "new" | null;
  onClose: () => void;
  apply: Props["apply"];
  onQuickMove: Props["onQuickMove"];
}) {
  const toast = useToast();
  const existing = part && part !== "new" ? part : null;
  const [error, setError] = useState("");

  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const text = (key: string) => String(form.get(key) ?? "");
    const num = (key: string) => (text(key).trim() === "" ? Number.NaN : Number(text(key)));
    const input = { oem: text("oem"), shelf: text("shelf"), name: text("name"), min: num("min") };
    const err = existing
      ? apply((d) => updatePart(d, existing.id, input))
      : apply((d) => addPart(d, { ...input, qty: num("qty") }));
    if (err) return setError(err);
    setError("");
    toast(existing ? "Parça güncellendi" : "Parça eklendi");
    onClose();
  }

  return (
    <Modal
      open={part !== null}
      title={existing ? existing.name : "Yeni parça"}
      onClose={() => {
        setError("");
        onClose();
      }}
    >
      <form onSubmit={submit} noValidate className="grid gap-3" key={existing?.id ?? "new"}>
        <div className="grid gap-2.5 sm:grid-cols-2">
          <Field label="OEM / parça kodu">
            <input name="oem" className="font-mono" defaultValue={existing?.oem} autoComplete="off" required />
          </Field>
          <Field label="Raf adresi" hint="örn. A-03-2">
            <input name="shelf" className="font-mono" defaultValue={existing?.shelf} autoComplete="off" required />
          </Field>
        </div>
        <Field label="Parça adı">
          <input name="name" defaultValue={existing?.name} autoComplete="off" required />
        </Field>
        <div className="grid gap-2.5 sm:grid-cols-2">
          {existing ? (
            <Field label="Mevcut adet" hint="hareketle değişir">
              <input value={existing.qty} disabled readOnly />
            </Field>
          ) : (
            <Field label="Başlangıç adedi">
              <input name="qty" type="number" inputMode="numeric" min={0} step={1} required />
            </Field>
          )}
          <Field label="Kritik seviye">
            <input name="min" type="number" inputMode="numeric" min={0} step={1} defaultValue={existing?.min} required />
          </Field>
        </div>
        <ErrorText>{error}</ErrorText>
        <Button type="submit" variant="primary" className="w-full text-lg">
          Kaydet
        </Button>
      </form>
      {existing && (
        <div className="flex flex-wrap gap-2">
          <Button className="flex-1" onClick={() => onQuickMove("in", existing.id)}>
            Giriş yap
          </Button>
          <Button className="flex-1" onClick={() => onQuickMove("out", existing.id)}>
            Çıkış yap
          </Button>
        </div>
      )}
    </Modal>
  );
}
