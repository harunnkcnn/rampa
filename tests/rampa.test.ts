import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { demoData } from "../src/domain/demo";
import {
  addPart,
  advanceShipment,
  createShipment,
  emptyData,
  loadingList,
  mergeItems,
  parseBackup,
  recordMovement,
  searchParts,
  sortForAttention,
  stockLevel,
  updatePart,
} from "../src/domain/rampa";
import type { RampaData, Result } from "../src/domain/types";

let counter = 0;
const id = () => `id-${++counter}`;
const now = () => 1_700_000_000_000;

function unwrap<T>(result: Result<T>): T {
  if (!result.ok) throw new Error(`Beklenmeyen hata: ${result.error}`);
  return result.value;
}

function withPart(qty = 10, min = 3): { data: RampaData; partId: string } {
  const data = unwrap(addPart(emptyData(), { oem: "1k0 698 151", name: "Balata", shelf: "a-01-2", qty, min }, id));
  return { data, partId: data.parts[0]!.id };
}

describe("stockLevel", () => {
  it("adet sıfırsa tükendi sayar", () => assert.equal(stockLevel({ qty: 0, min: 4 }), "out"));
  it("kritik seviyeye eşit veya altındaysa kritik sayar", () => {
    assert.equal(stockLevel({ qty: 4, min: 4 }), "critical");
    assert.equal(stockLevel({ qty: 1, min: 4 }), "critical");
  });
  it("kritik seviyenin 1,5 katına kadar düşük sayar", () => assert.equal(stockLevel({ qty: 6, min: 4 }), "low"));
  it("üstünde yeterli sayar", () => assert.equal(stockLevel({ qty: 7, min: 4 }), "ok"));
});

describe("addPart / updatePart", () => {
  it("kodu ve rafı büyük harfe çevirir", () => {
    const { data } = withPart();
    assert.equal(data.parts[0]!.oem, "1K0 698 151");
    assert.equal(data.parts[0]!.shelf, "A-01-2");
  });
  it("aynı kodu boşluk farkıyla bile ikinci kez eklemez", () => {
    const { data } = withPart();
    const result = addPart(data, { oem: "1K0698151", name: "Başka", shelf: "B-01-1", qty: 1, min: 0 }, id);
    assert.equal(result.ok, false);
  });
  it("negatif veya ondalıklı adedi reddeder", () => {
    assert.equal(addPart(emptyData(), { oem: "X", name: "Y", shelf: "Z", qty: -1, min: 0 }).ok, false);
    assert.equal(addPart(emptyData(), { oem: "X", name: "Y", shelf: "Z", qty: 1.5, min: 0 }).ok, false);
  });
  it("güncellemede adedi değiştirmez", () => {
    const { data, partId } = withPart(10);
    const next = unwrap(updatePart(data, partId, { oem: "1K0 698 151", name: "Ön balata", shelf: "A-02-1", min: 5 }));
    assert.equal(next.parts[0]!.qty, 10);
    assert.equal(next.parts[0]!.name, "Ön balata");
  });
});

describe("recordMovement", () => {
  it("girişte stoku artırır, çıkışta azaltır", () => {
    const { data, partId } = withPart(10);
    const afterIn = unwrap(recordMovement(data, { type: "in", partId, qty: 5, doc: "irs-1" }, id, now));
    assert.equal(afterIn.parts[0]!.qty, 15);
    assert.equal(afterIn.movements[0]!.doc, "IRS-1");
    const afterOut = unwrap(recordMovement(afterIn, { type: "out", partId, qty: 15, doc: "IRS-2" }, id, now));
    assert.equal(afterOut.parts[0]!.qty, 0);
  });
  it("stokta olmayan parçayı çıkarmaz", () => {
    const { data, partId } = withPart(2);
    const result = recordMovement(data, { type: "out", partId, qty: 3, doc: "IRS-1" });
    assert.equal(result.ok, false);
  });
  it("belge numarası olmadan kaydetmez", () => {
    const { data, partId } = withPart();
    assert.equal(recordMovement(data, { type: "in", partId, qty: 1, doc: "  " }).ok, false);
  });
  it("orijinal veriyi değiştirmez", () => {
    const { data, partId } = withPart(10);
    recordMovement(data, { type: "out", partId, qty: 1, doc: "IRS-1" });
    assert.equal(data.parts[0]!.qty, 10);
  });
});

describe("sevkiyat", () => {
  it("aynı parçanın kalemlerini birleştirir", () => {
    assert.deepEqual(mergeItems([{ partId: "a", qty: 1 }, { partId: "a", qty: 2 }, { partId: "b", qty: 1 }]), [
      { partId: "a", qty: 3 },
      { partId: "b", qty: 1 },
    ]);
  });
  it("stoktan fazlasıyla sevkiyat oluşturmaz", () => {
    const { data, partId } = withPart(2);
    const result = createShipment(data, { customer: "Servis", plate: "34 abc 1", doc: "IRS-9", items: [{ partId, qty: 3 }] });
    assert.equal(result.ok, false);
  });
  it("Yüklendi adımında stoktan düşer ve teslime kadar ilerler", () => {
    const { data, partId } = withPart(10);
    let state = unwrap(
      createShipment(data, { customer: "Servis", plate: "34 abc 1", doc: "irs-9", items: [{ partId, qty: 4 }] }, id, now),
    );
    const shipmentId = state.shipments[0]!.id;
    assert.equal(state.parts[0]!.qty, 10, "oluştururken stok düşmez");

    state = unwrap(advanceShipment(state, shipmentId, id, now));
    assert.equal(state.shipments[0]!.status, 1);
    assert.equal(state.parts[0]!.qty, 6);
    assert.equal(state.movements.at(-1)!.doc, "IRS-9");

    state = unwrap(advanceShipment(state, shipmentId, id, now));
    state = unwrap(advanceShipment(state, shipmentId, id, now));
    assert.equal(state.shipments[0]!.status, 3);
    assert.equal(state.parts[0]!.qty, 6, "sonraki adımlarda tekrar düşmez");
    assert.equal(advanceShipment(state, shipmentId).ok, false);
  });
  it("yüklemeden önce stok azaldıysa yüklemeye izin vermez", () => {
    const { data, partId } = withPart(5);
    let state = unwrap(createShipment(data, { customer: "S", plate: "P", doc: "D", items: [{ partId, qty: 5 }] }, id, now));
    state = unwrap(recordMovement(state, { type: "out", partId, qty: 1, doc: "X" }, id, now));
    assert.equal(advanceShipment(state, state.shipments[0]!.id).ok, false);
  });
  it("yükleme listesinde raf ve toplam adet var", () => {
    const { data, partId } = withPart(10);
    const state = unwrap(createShipment(data, { customer: "Servis", plate: "34 ABC 1", doc: "IRS-9", items: [{ partId, qty: 4 }] }, id, now));
    const text = loadingList(state, state.shipments[0]!);
    assert.match(text, /Raf A-01-2/);
    assert.match(text, /Toplam: 4 adet/);
  });
});

describe("yardımcılar", () => {
  it("demo verisi tutarlı", () => {
    const data = demoData(now(), id);
    const ids = new Set(data.parts.map((p) => p.id));
    for (const s of data.shipments) for (const item of s.items) assert.ok(ids.has(item.partId));
  });
  it("arama koddaki boşlukları yok sayar", () => {
    const data = demoData(now(), id);
    assert.equal(searchParts(data.parts, "1k0698151").length, 1);
  });
  it("acil parçalar önce gelir", () => {
    const data = demoData(now(), id);
    assert.equal(stockLevel(sortForAttention(data.parts)[0]!), "out");
  });
  it("bozuk yedeği reddeder", () => {
    assert.equal(parseBackup("{").ok, false);
    assert.equal(parseBackup('{"parts":[]}').ok, false);
    assert.equal(parseBackup(JSON.stringify(emptyData())).ok, true);
  });
});
