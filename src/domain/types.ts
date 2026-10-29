export type MovementType = "in" | "out";

export interface Part {
  id: string;
  /** OEM veya üretici parça kodu, örn. "1K0 698 151" */
  oem: string;
  name: string;
  /** Raf adresi: koridor-raf-göz, örn. "A-03-2" */
  shelf: string;
  qty: number;
  /** Bu adede veya altına inince parça kritik sayılır */
  min: number;
}

export interface Movement {
  id: string;
  ts: number;
  type: MovementType;
  partId: string;
  qty: number;
  /** İrsaliye veya iş emri numarası */
  doc: string;
  note: string;
}

export const SHIPMENT_STATUSES = ["Hazırlanıyor", "Yüklendi", "Yolda", "Teslim edildi"] as const;
export type ShipmentStatus = 0 | 1 | 2 | 3;

export interface ShipmentItem {
  partId: string;
  qty: number;
}

export interface Shipment {
  id: string;
  customer: string;
  plate: string;
  driver: string;
  doc: string;
  items: ShipmentItem[];
  status: ShipmentStatus;
  createdAt: number;
  history: { status: ShipmentStatus; ts: number }[];
}

export interface RampaData {
  version: 1;
  parts: Part[];
  movements: Movement[];
  shipments: Shipment[];
}

export type StockLevel = "out" | "critical" | "low" | "ok";

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };
