import { useMemo, useState, type FormEvent } from "react";
import { advanceShipment, createShipment, loadingList } from "../domain/rampa";
import { SHIPMENT_STATUSES, type RampaData, type Result, type Shipment } from "../domain/types";
import { cx, formatDateTime } from "../lib/format";
import { Button, Card, EmptyState, ErrorText, Field, Modal, Pill, Segmented, type PillTone } from "./ui";
import { useToast } from "./Toast";

type Filter = "open" | "done";

const NEXT_ACTION = ["Yüklendi olarak işaretle", "Yola çıktı", "Teslim edildi"] as const;
const STATUS_TONE: PillTone[] = ["low", "info", "info", "ok"];

interface Props {
  data: RampaData;
  apply: (op: (d: RampaData) => Result<RampaData>) => string | null;
}

export function ShipmentView({ data, apply }: Props) {
  const toast = useToast();
  const [filter, setFilter] = useState<Filter>("open");
  const [creating, setCreating] = useState(false);

  const visible = useMemo(
    () =>
      data.shipments
        .filter((s) => (filter === "open" ? s.status < 3 : s.status === 3))
        .sort((a, b) => b.createdAt - a.createdAt),
    [data.shipments, filter],
  );

  async function shareList(shipment: Shipment) {
    const text = loadingList(data, shipment);
    if (navigator.share) {
      try {
        await navigator.share({ title: `Yükleme listesi ${shipment.doc}`, text });
        return;
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") return;
      }
    }
    try {
      await navigator.clipboard.writeText(text);
      toast("Yükleme listesi kopyalandı");
    } catch {
      window.prompt("Yükleme listesi", text);
    }
  }

  function advance(shipment: Shipment) {
    const err = apply((d) => advanceShipment(d, shipment.id));
    toast(err ?? SHIPMENT_STATUSES[Math.min(shipment.status + 1, 3)]);
  }

  return (
    <section>
      <div className="mt-5 flex items-center justify-between gap-2">
        <h2 className="text-2xl">Sevkiyat</h2>
        <Button
          variant="dark"
          onClick={() => (data.parts.length ? setCreating(true) : toast("Önce stoka parça ekleyin"))}
        >
          + Yeni sevkiyat
        </Button>
      </div>
      <div className="mt-3">
        <Segmented
          label="Sevkiyat filtresi"
          value={filter}
          onChange={setFilter}
          options={[
            { value: "open", label: "Açık" },
            { value: "done", label: "Teslim edilen" },
          ]}
        />
      </div>

      <div className="my-3 flex flex-col gap-2">
        {visible.length === 0 ? (
          <EmptyState>{filter === "open" ? "Açık sevkiyat yok." : "Teslim edilen sevkiyat yok."}</EmptyState>
        ) : (
          visible.map((s) => (
            <ShipmentCard key={s.id} shipment={s} data={data} onShare={() => shareList(s)} onAdvance={() => advance(s)} />
          ))
        )}
      </div>

      <NewShipmentDialog
        open={creating}
        data={data}
        onClose={() => setCreating(false)}
        onCreate={(input) => {
          const err = apply((d) => createShipment(d, input));
          if (!err) {
            toast("Sevkiyat oluşturuldu");
            setFilter("open");
            setCreating(false);
          }
          return err;
        }}
      />
    </section>
  );
}

function ShipmentCard({
  shipment: s,
  data,
  onShare,
  onAdvance,
}: {
  shipment: Shipment;
  data: RampaData;
  onShare: () => void;
  onAdvance: () => void;
}) {
  const delivered = s.status === 3;
  return (
    <Card>
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="text-lg">{s.customer}</h3>
          <div className="text-sm text-muted">
            <span className="font-mono">{s.plate}</span> · <span className="font-mono">{s.doc}</span>
            {s.driver && ` · ${s.driver}`}
          </div>
        </div>
        <Pill tone={STATUS_TONE[s.status]}>{SHIPMENT_STATUSES[s.status]}</Pill>
      </div>

      <ol className="mt-3 grid grid-cols-4 gap-1" aria-label={`Durum: ${SHIPMENT_STATUSES[s.status]}`}>
        {SHIPMENT_STATUSES.map((label, i) => (
          <li key={label} className="text-[10px] uppercase tracking-wide text-muted">
            <div className={cx("mb-1 h-1.5 rounded", i <= s.status ? "bg-safety" : "bg-line")} />
            {label}
          </li>
        ))}
      </ol>

      <ul className="mt-2.5 text-sm">
        {s.items.map((item) => {
          const part = data.parts.find((p) => p.id === item.partId);
          return (
            <li key={item.partId} className="flex justify-between gap-2 border-t border-line py-1.5">
              <span>
                {part?.name ?? "Bilinmeyen parça"} <span className="font-mono text-muted">{part?.shelf}</span>
              </span>
              <b>{item.qty}</b>
            </li>
          );
        })}
      </ul>

      <div className="mt-2.5 flex flex-wrap gap-2">
        <Button className="flex-[1_1_140px]" onClick={onShare}>
          Yükleme listesi
        </Button>
        {delivered ? (
          <span className="self-center text-sm text-muted">Teslim: {formatDateTime(s.history[s.history.length - 1]?.ts ?? s.createdAt)}</span>
        ) : (
          <Button variant="primary" className="flex-[2_1_180px]" onClick={onAdvance}>
            {NEXT_ACTION[s.status as 0 | 1 | 2]}
          </Button>
        )}
      </div>
    </Card>
  );
}

interface Row {
  key: number;
  partId: string;
  qty: string;
}

function NewShipmentDialog({
  open,
  data,
  onClose,
  onCreate,
}: {
  open: boolean;
  data: RampaData;
  onClose: () => void;
  onCreate: (input: Parameters<typeof createShipment>[1]) => string | null;
}) {
  const sortedParts = useMemo(() => [...data.parts].sort((a, b) => a.name.localeCompare(b.name, "tr")), [data.parts]);
  const firstPart = sortedParts[0]?.id ?? "";
  const [rows, setRows] = useState<Row[]>([{ key: 0, partId: firstPart, qty: "1" }]);
  const [error, setError] = useState("");

  function reset() {
    setRows([{ key: Date.now(), partId: firstPart, qty: "1" }]);
    setError("");
  }

  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const text = (key: string) => String(form.get(key) ?? "");
    const err = onCreate({
      customer: text("customer"),
      plate: text("plate"),
      driver: text("driver"),
      doc: text("doc"),
      items: rows.map((r) => ({ partId: r.partId || firstPart, qty: r.qty.trim() === "" ? Number.NaN : Number(r.qty) })),
    });
    if (err) return setError(err);
    reset();
  }

  return (
    <Modal
      open={open}
      title="Yeni sevkiyat"
      onClose={() => {
        reset();
        onClose();
      }}
    >
      <form onSubmit={submit} noValidate className="grid gap-3">
        <Field label="Alıcı" hint="bayi / servis">
          <input name="customer" autoComplete="off" required />
        </Field>
        <div className="grid gap-2.5 sm:grid-cols-2">
          <Field label="Araç plakası">
            <input name="plate" className="font-mono" placeholder="34 ABC 123" autoComplete="off" required />
          </Field>
          <Field label="Şoför">
            <input name="driver" autoComplete="off" />
          </Field>
        </div>
        <Field label="İrsaliye no">
          <input name="doc" className="font-mono" defaultValue={`IRS-${String(Date.now()).slice(-6)}`} autoComplete="off" required />
        </Field>

        <fieldset className="grid gap-2">
          <legend className="mb-1.5 text-sm font-semibold">Kalemler</legend>
          {rows.map((row, index) => (
            <div key={row.key} className="grid grid-cols-[1fr_88px_44px] items-end gap-2">
              <Field label={`Parça ${index + 1}`}>
                <select
                  value={row.partId}
                  onChange={(e) => setRows(rows.map((r) => (r.key === row.key ? { ...r, partId: e.target.value } : r)))}
                >
                  {sortedParts.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} · stok {p.qty}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Adet">
                <input
                  type="number"
                  inputMode="numeric"
                  min={1}
                  step={1}
                  value={row.qty}
                  onChange={(e) => setRows(rows.map((r) => (r.key === row.key ? { ...r, qty: e.target.value } : r)))}
                />
              </Field>
              <button
                type="button"
                aria-label={`Kalem ${index + 1}'i sil`}
                disabled={rows.length === 1}
                onClick={() => setRows(rows.filter((r) => r.key !== row.key))}
                className="h-12 cursor-pointer rounded-lg border border-line text-xl disabled:opacity-40"
              >
                ×
              </button>
            </div>
          ))}
          <Button
            className="justify-self-start"
            onClick={() => setRows([...rows, { key: Date.now(), partId: firstPart, qty: "1" }])}
          >
            + Kalem ekle
          </Button>
        </fieldset>

        <ErrorText>{error}</ErrorText>
        <Button type="submit" variant="primary" className="w-full text-lg">
          Sevkiyatı oluştur
        </Button>
      </form>
    </Modal>
  );
}
