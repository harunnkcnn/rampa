import {
  SHIPMENT_STATUSES,
  type Movement,
  type MovementType,
  type Part,
  type RampaData,
  type Result,
  type Shipment,
  type ShipmentItem,
  type ShipmentStatus,
  type StockLevel,
} from "./types";

export type IdFactory = () => string;
export type Clock = () => number;

export const defaultId: IdFactory = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2, 12);

const ok = <T>(value: T): Result<T> => ({ ok: true, value });
const fail = <T>(error: string): Result<T> => ({ ok: false, error });

/**
 * Parça kodu, raf, plaka ve belge numaraları için. Türkçe yerel ayar kullanılmaz:
 * "irs-1" → "IRS-1" olmalı, "İRS-1" değil.
 */
export const toCode = (s: string): string => s.trim().toUpperCase();

/** Karşılaştırma için: büyük/küçük harf ve boşluk farklarını yok sayar */
export const normalizeCode = (s: string): string => s.toUpperCase().replace(/\s+/g, "");

export const emptyData = (): RampaData => ({ version: 1, parts: [], movements: [], shipments: [] });

export function stockLevel(part: Pick<Part, "qty" | "min">): StockLevel {
  if (part.qty <= 0) return "out";
  if (part.qty <= part.min) return "critical";
  if (part.qty <= Math.ceil(part.min * 1.5)) return "low";
  return "ok";
}

export const isAlert = (part: Pick<Part, "qty" | "min">): boolean => {
  const level = stockLevel(part);
  return level === "out" || level === "critical";
};

const LEVEL_ORDER: Record<StockLevel, number> = { out: 0, critical: 1, low: 2, ok: 3 };

/** Önce en acil parçalar, sonra raf adresine göre */
export function sortForAttention(parts: Part[]): Part[] {
  return [...parts].sort(
    (a, b) => LEVEL_ORDER[stockLevel(a)] - LEVEL_ORDER[stockLevel(b)] || a.shelf.localeCompare(b.shelf, "tr"),
  );
}

export function searchParts(parts: Part[], query: string): Part[] {
  const q = query.toLocaleLowerCase("tr-TR").replace(/\s+/g, "");
  if (!q) return parts;
  return parts.filter((p) => (p.oem + p.name + p.shelf).toLocaleLowerCase("tr-TR").replace(/\s+/g, "").includes(q));
}

export interface PartInput {
  oem: string;
  name: string;
  shelf: string;
  qty: number;
  min: number;
}

export function addPart(data: RampaData, input: PartInput, id: IdFactory = defaultId): Result<RampaData> {
  const oem = toCode(input.oem);
  const shelf = toCode(input.shelf);
  const name = input.name.trim();
  if (!oem || !shelf || !name) return fail("Parça kodu, raf adresi ve ad zorunlu.");
  if (!Number.isInteger(input.qty) || input.qty < 0) return fail("Başlangıç adedi 0 veya daha büyük bir tam sayı olmalı.");
  if (!Number.isInteger(input.min) || input.min < 0) return fail("Kritik seviye 0 veya daha büyük bir tam sayı olmalı.");
  if (data.parts.some((p) => normalizeCode(p.oem) === normalizeCode(oem))) return fail("Bu kodla bir parça zaten var.");
  const part: Part = { id: id(), oem, shelf, name, qty: input.qty, min: input.min };
  return ok({ ...data, parts: [...data.parts, part] });
}

export function updatePart(
  data: RampaData,
  partId: string,
  input: Omit<PartInput, "qty">,
): Result<RampaData> {
  const current = data.parts.find((p) => p.id === partId);
  if (!current) return fail("Parça bulunamadı.");
  const oem = toCode(input.oem);
  const shelf = toCode(input.shelf);
  const name = input.name.trim();
  if (!oem || !shelf || !name) return fail("Parça kodu, raf adresi ve ad zorunlu.");
  if (!Number.isInteger(input.min) || input.min < 0) return fail("Kritik seviye 0 veya daha büyük bir tam sayı olmalı.");
  if (data.parts.some((p) => p.id !== partId && normalizeCode(p.oem) === normalizeCode(oem)))
    return fail("Bu kodla bir parça zaten var.");
  return ok({ ...data, parts: data.parts.map((p) => (p.id === partId ? { ...p, oem, shelf, name, min: input.min } : p)) });
}

export interface MovementInput {
  type: MovementType;
  partId: string;
  qty: number;
  doc: string;
  note?: string;
}

/**
 * Stok hareketi kaydeder. Belge numarası zorunludur ve stokta olmayan parça çıkarılamaz.
 */
export function recordMovement(
  data: RampaData,
  input: MovementInput,
  id: IdFactory = defaultId,
  now: Clock = Date.now,
): Result<RampaData> {
  const part = data.parts.find((p) => p.id === input.partId);
  if (!part) return fail("Parça bulunamadı.");
  if (!Number.isInteger(input.qty) || input.qty <= 0) return fail("Adet 1 veya daha büyük bir tam sayı olmalı.");
  const doc = toCode(input.doc);
  if (!doc) return fail("Belge numarası olmadan hareket kaydedilmez.");
  if (input.type === "out" && input.qty > part.qty) return fail(`Stokta yeterli parça yok (mevcut ${part.qty}).`);

  const movement: Movement = {
    id: id(),
    ts: now(),
    type: input.type,
    partId: part.id,
    qty: input.qty,
    doc,
    note: (input.note ?? "").trim(),
  };
  const delta = input.type === "in" ? input.qty : -input.qty;
  return ok({
    ...data,
    parts: data.parts.map((p) => (p.id === part.id ? { ...p, qty: p.qty + delta } : p)),
    movements: [...data.movements, movement],
  });
}

export interface ShipmentInput {
  customer: string;
  plate: string;
  driver?: string;
  doc: string;
  items: ShipmentItem[];
}

/** Aynı parçanın tekrar eden kalemlerini birleştirir */
export function mergeItems(items: ShipmentItem[]): ShipmentItem[] {
  const totals = new Map<string, number>();
  for (const item of items) totals.set(item.partId, (totals.get(item.partId) ?? 0) + item.qty);
  return [...totals].map(([partId, qty]) => ({ partId, qty }));
}

function checkAvailability(data: RampaData, items: ShipmentItem[]): string | null {
  for (const item of items) {
    const part = data.parts.find((p) => p.id === item.partId);
    if (!part) return "Kalemlerden biri stokta bulunamadı.";
    if (part.qty < item.qty) return `Stok yetersiz: ${part.name} (mevcut ${part.qty}).`;
  }
  return null;
}

export function createShipment(
  data: RampaData,
  input: ShipmentInput,
  id: IdFactory = defaultId,
  now: Clock = Date.now,
): Result<RampaData> {
  const customer = input.customer.trim();
  const plate = toCode(input.plate);
  const doc = toCode(input.doc);
  if (!customer || !plate || !doc) return fail("Alıcı, plaka ve irsaliye no zorunlu.");
  if (input.items.length === 0) return fail("En az bir kalem ekleyin.");
  if (input.items.some((i) => !Number.isInteger(i.qty) || i.qty <= 0)) return fail("Her kalemin adedi 1 veya daha büyük olmalı.");
  const items = mergeItems(input.items);
  const shortage = checkAvailability(data, items);
  if (shortage) return fail(shortage);

  const ts = now();
  const shipment: Shipment = {
    id: id(),
    customer,
    plate,
    driver: (input.driver ?? "").trim(),
    doc,
    items,
    status: 0,
    createdAt: ts,
    history: [{ status: 0, ts }],
  };
  return ok({ ...data, shipments: [...data.shipments, shipment] });
}

/**
 * Sevkiyatı bir sonraki duruma taşır. "Yüklendi" adımında kalemler stoktan
 * irsaliye numarasıyla düşülür.
 */
export function advanceShipment(
  data: RampaData,
  shipmentId: string,
  id: IdFactory = defaultId,
  now: Clock = Date.now,
): Result<RampaData> {
  const shipment = data.shipments.find((s) => s.id === shipmentId);
  if (!shipment) return fail("Sevkiyat bulunamadı.");
  if (shipment.status >= 3) return fail("Sevkiyat zaten teslim edildi.");

  let next = data;
  if (shipment.status === 0) {
    const shortage = checkAvailability(data, shipment.items);
    if (shortage) return fail(shortage);
    for (const item of shipment.items) {
      const result = recordMovement(
        next,
        { type: "out", partId: item.partId, qty: item.qty, doc: shipment.doc, note: `Sevkiyat · ${shipment.customer}` },
        id,
        now,
      );
      if (!result.ok) return result;
      next = result.value;
    }
  }
  const status = (shipment.status + 1) as ShipmentStatus;
  const ts = now();
  return ok({
    ...next,
    shipments: next.shipments.map((s) =>
      s.id === shipmentId ? { ...s, status, history: [...s.history, { status, ts }] } : s,
    ),
  });
}

export function loadingList(data: RampaData, shipment: Shipment): string {
  const lines = [
    `YÜKLEME LİSTESİ · ${shipment.doc}`,
    `Alıcı: ${shipment.customer}`,
    `Araç: ${shipment.plate}${shipment.driver ? ` · ${shipment.driver}` : ""}`,
    "",
  ];
  shipment.items.forEach((item, i) => {
    const part = data.parts.find((p) => p.id === item.partId);
    lines.push(
      part
        ? `${i + 1}. ${part.name} (${part.oem}) · Raf ${part.shelf} · ${item.qty} adet`
        : `${i + 1}. Bilinmeyen parça · ${item.qty} adet`,
    );
  });
  const total = shipment.items.reduce((sum, item) => sum + item.qty, 0);
  lines.push("", `Toplam: ${total} adet`, `Durum: ${SHIPMENT_STATUSES[shipment.status]}`);
  return lines.join("\n");
}

/** Yedekten gelen verinin temel yapısını doğrular */
export function parseBackup(raw: string): Result<RampaData> {
  try {
    const value: unknown = JSON.parse(raw);
    if (
      typeof value === "object" &&
      value !== null &&
      Array.isArray((value as RampaData).parts) &&
      Array.isArray((value as RampaData).movements) &&
      Array.isArray((value as RampaData).shipments)
    ) {
      return ok({ ...(value as RampaData), version: 1 });
    }
    return fail("Geçerli bir Rampa yedeği değil.");
  } catch {
    return fail("Dosya okunamadı.");
  }
}
