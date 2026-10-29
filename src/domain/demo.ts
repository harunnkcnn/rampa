import type { Part, RampaData } from "./types";
import { defaultId, type IdFactory } from "./rampa";

/** İlk açılış için örnek veri. Parça kodları ve alıcılar örnektir. */
export function demoData(now: number = Date.now(), id: IdFactory = defaultId): RampaData {
  const hour = 3_600_000;
  const part = (oem: string, name: string, shelf: string, qty: number, min: number): Part => ({
    id: id(),
    oem,
    name,
    shelf,
    qty,
    min,
  });

  const parts: Part[] = [
    part("1K0 698 151", "Ön fren balatası takımı", "A-01-2", 24, 8),
    part("1K0 615 301", "Ön fren diski", "A-01-4", 6, 6),
    part("03L 115 562", "Yağ filtresi", "B-02-1", 40, 12),
    part("5Q0 129 620", "Hava filtresi", "B-02-3", 3, 10),
    part("6R0 820 367", "Polen filtresi", "B-03-1", 15, 6),
    part("7M3 413 031", "Ön amortisör", "C-04-2", 0, 4),
    part("000 915 105", "Akü 12V 72Ah", "D-01-1", 9, 5),
    part("5K0 955 427", "Silecek süpürgesi takımı", "C-02-2", 18, 6),
    part("04E 905 612", "Buji (4'lü)", "B-01-4", 7, 6),
    part("1J0 407 151", "Salıncak kolu", "C-05-1", 11, 4),
  ];
  const by = (oem: string): string => {
    const found = parts.find((p) => p.oem === oem);
    if (!found) throw new Error(`Demo parçası yok: ${oem}`);
    return found.id;
  };

  return {
    version: 1,
    parts,
    movements: [
      { id: id(), ts: now - 30 * hour, type: "in", partId: by("03L 115 562"), qty: 24, doc: "IRS-104512", note: "Tedarikçi teslimatı" },
      { id: id(), ts: now - 26 * hour, type: "out", partId: by("1K0 698 151"), qty: 4, doc: "ISE-2207", note: "Servis iş emri" },
      { id: id(), ts: now - 20 * hour, type: "out", partId: by("5Q0 129 620"), qty: 6, doc: "IRS-104530", note: "" },
      { id: id(), ts: now - 5 * hour, type: "in", partId: by("000 915 105"), qty: 6, doc: "IRS-104571", note: "" },
    ],
    shipments: [
      {
        id: id(),
        customer: "Örnek Oto Servis",
        plate: "34 RMP 101",
        driver: "Sürücü A",
        doc: "IRS-104580",
        status: 0,
        createdAt: now - 3 * hour,
        items: [
          { partId: by("1K0 698 151"), qty: 4 },
          { partId: by("03L 115 562"), qty: 10 },
        ],
        history: [{ status: 0, ts: now - 3 * hour }],
      },
      {
        id: id(),
        customer: "Demo Bayi Merkez",
        plate: "34 RMP 202",
        driver: "Sürücü B",
        doc: "IRS-104566",
        status: 2,
        createdAt: now - 9 * hour,
        items: [
          { partId: by("5K0 955 427"), qty: 6 },
          { partId: by("6R0 820 367"), qty: 5 },
        ],
        history: [
          { status: 0, ts: now - 9 * hour },
          { status: 1, ts: now - 7 * hour },
          { status: 2, ts: now - 6 * hour },
        ],
      },
    ],
  };
}
