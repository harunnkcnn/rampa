package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// warehouse2 creates a second warehouse with bins A-01 and A-02.
func warehouse2(t *testing.T, s *Store) (wh, a1, a2 int64) {
	t.Helper()
	ctx := context.Background()
	br, err := s.CreateBranch(ctx, "İzmir Kuzey")
	if err != nil {
		t.Fatal(err)
	}
	wh, err = s.CreateWarehouse(ctx, br, "06", "Satış depo")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.CreateBins(ctx, wh, []string{"a-01", " A-02 ", "A-01", ""}); err != nil || n != 2 {
		t.Fatalf("CreateBins = %d, %v", n, err)
	}
	b1, _ := s.FindBin(ctx, wh, "a-01")
	b2, _ := s.FindBin(ctx, wh, "A-02")
	return wh, b1.ID, b2.ID
}

func TestMigrationKeepsExistingStock(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A database created by v0.1.0.
	if err := s.Migrate(ctx, CoreMigrations[:1]); err != nil {
		t.Fatal(err)
	}
	s.DB.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES ('depo', 'x', 0, 0)`)
	s.DB.Exec(`INSERT INTO products (sku, name, created_at) VALUES ('P1', 'Ürün', 0)`)
	s.DB.Exec(`INSERT INTO movements (product_id, kind, qty, created_at) VALUES (1, 'in', 12000, 0), (1, 'out', -2000, 0)`)
	s.DB.Exec(`INSERT INTO shipments (code, customer, created_at) VALUES ('SVK-1', 'X', 0)`)

	if err := s.Migrate(ctx, CoreMigrations); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	p, err := s.GetProduct(ctx, 1)
	if err != nil || p.Stock != QtyFromInt(10) {
		t.Fatalf("stock after upgrade = %v, %v", p.Stock, err)
	}
	if bins, _ := s.ProductBins(ctx, 1, 0); len(bins) != 1 || bins[0].BinCode != "01/GENEL" {
		t.Fatalf("old stock should land in 01/GENEL: %+v", bins)
	}
	sh, err := s.GetShipment(ctx, 1)
	if err != nil || sh.WarehouseID != 1 {
		t.Fatalf("shipment warehouse = %d, %v", sh.WarehouseID, err)
	}
	ws, _ := s.UserWarehouses(ctx, 1, false)
	if len(ws) != 1 {
		t.Fatalf("existing user should keep access to the main warehouse, got %d", len(ws))
	}
}

func TestBinsAndTransfer(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := mustProduct(t, s, "AKU", 0)
	wh, a1, a2 := warehouse2(t, s)

	if _, err := s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: a1, Kind: MoveIn, Qty: QtyFromInt(10)}); err != nil {
		t.Fatal(err)
	}
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: 1, Kind: MoveIn, Qty: QtyFromInt(5)})

	// Out-checks are per bin, not per product.
	_, err := s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: a2, Kind: MoveOut, Qty: QtyFromInt(1)})
	var ise *InsufficientStockError
	if !errors.As(err, &ise) || ise.Bin != "A-02" {
		t.Fatalf("empty bin should refuse, got %v", err)
	}
	if err := s.Transfer(ctx, id, a1, a2, QtyFromInt(4), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Transfer(ctx, id, a1, 1, QtyFromInt(1), 0); !errors.Is(err, ErrWrongWarehouse) {
		t.Fatalf("cross-warehouse transfer should be refused, got %v", err)
	}
	bins, _ := s.ProductBins(ctx, id, wh)
	if len(bins) != 2 || bins[0].Qty != QtyFromInt(6) || bins[1].Qty != QtyFromInt(4) {
		t.Fatalf("bins = %+v", bins)
	}
	scoped, _ := s.ListProducts(ctx, ProductFilter{WarehouseID: wh})
	all, _ := s.ListProducts(ctx, ProductFilter{})
	if scoped[0].Stock != QtyFromInt(10) || all[0].Stock != QtyFromInt(15) {
		t.Fatalf("scoped %v, all %v", scoped[0].Stock, all[0].Stock)
	}
	// Bin-level adjust: shelf A-02 actually holds 3.
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: a2, Kind: MoveAdjust, Qty: QtyFromInt(3)})
	if q, _ := binQty(ctx, s.DB, a2, id); q != QtyFromInt(3) {
		t.Fatalf("A-02 after count = %v", q)
	}
}

func TestBarcodeLookup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, err := s.CreateProduct(ctx, Product{SKU: "flt-1042", Barcode: "8690000 001042", Name: "Filtre"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProduct(ctx, Product{SKU: "X", Barcode: "8690000001042", Name: "Y"}); !errors.Is(err, ErrDuplicateBarcode) {
		t.Fatalf("want duplicate barcode, got %v", err)
	}
	for _, code := range []string{"8690000001042", "flt-1042", " FLT-1042 "} {
		p, err := s.FindProductByCode(ctx, code)
		if err != nil || p.ID != id {
			t.Fatalf("FindProductByCode(%q) = %v, %v", code, p.ID, err)
		}
	}
	if _, err := s.FindProductByCode(ctx, "999"); !errors.Is(err, ErrUnknownCode) {
		t.Fatalf("unknown code: %v", err)
	}
}

func TestReceiptFlow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := mustProduct(t, s, "P", 0)
	wh, a1, _ := warehouse2(t, s)
	rid, err := s.CreateReceipt(ctx, wh, "Bosch Türkiye", "abc-123", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Receive(ctx, rid, 1, id, QtyFromInt(1), 0); !errors.Is(err, ErrWrongWarehouse) {
		t.Fatalf("bin of another warehouse: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Receive(ctx, rid, a1, id, QtyFromInt(2), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UndoLastReceiptLine(ctx, rid, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.UndoLastReceiptLine(ctx, rid, 0); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetReceipt(ctx, rid)
	lines, _ := s.ReceiptLines(ctx, rid)
	if r.Units != QtyFromInt(2) || len(lines) != 1 || lines[0].Qty != QtyFromInt(2) || r.DocNo != "ABC-123" {
		t.Fatalf("receipt = %+v lines %+v", r, lines)
	}
	if err := s.CloseReceipt(ctx, rid); err != nil {
		t.Fatal(err)
	}
	if err := s.Receive(ctx, rid, a1, id, QtyFromInt(1), 0); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("closed receipt: %v", err)
	}
}

func TestBlindCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p1 := mustProduct(t, s, "P1", 0)
	p2 := mustProduct(t, s, "P2", 0)
	p3 := mustProduct(t, s, "P3", 0)
	_, a1, _ := warehouse2(t, s)
	s.RecordMovement(ctx, MovementInput{ProductID: p1, BinID: a1, Kind: MoveIn, Qty: QtyFromInt(10)})
	s.RecordMovement(ctx, MovementInput{ProductID: p2, BinID: a1, Kind: MoveIn, Qty: QtyFromInt(4)})

	cid, err := s.StartCount(ctx, a1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.StartCount(ctx, a1, 0); again != cid {
		t.Fatal("a bin has at most one open count")
	}
	s.AddCount(ctx, cid, p1, QtyFromInt(6))
	s.AddCount(ctx, cid, p1, QtyFromInt(4)) // P1 matches: 10
	s.AddCount(ctx, cid, p3, QtyFromInt(2)) // P3 found, not on the books; P2 missing
	if _, err := s.AddCount(ctx, cid, p3, QtyFromInt(-5)); !errors.Is(err, ErrInvalidQty) {
		t.Fatalf("count cannot go negative: %v", err)
	}
	c, _ := s.GetCount(ctx, cid)
	if d := c.Differences(); len(d) != 2 {
		t.Fatalf("differences = %+v", d)
	}
	if n, err := s.FinishCount(ctx, cid, 0); err != nil || n != 2 {
		t.Fatalf("FinishCount = %d, %v", n, err)
	}
	for id, want := range map[int64]int64{p1: 10, p2: 0, p3: 2} {
		if q, _ := binQty(ctx, s.DB, a1, id); q != QtyFromInt(want) {
			t.Errorf("product %d = %v, want %d", id, q, want)
		}
	}
}

func TestPickingAndLoadFromBins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := mustProduct(t, s, "P", 0)
	wh, a1, a2 := warehouse2(t, s)
	gen, _ := s.DefaultBin(ctx, wh)
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: a1, Kind: MoveIn, Qty: QtyFromInt(3)})
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: a2, Kind: MoveIn, Qty: QtyFromInt(5)})
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: gen.ID, Kind: MoveIn, Qty: QtyFromInt(5)})
	s.RecordMovement(ctx, MovementInput{ProductID: id, BinID: 1, Kind: MoveIn, Qty: QtyFromInt(100)}) // other warehouse

	sid, _ := s.CreateShipment(ctx, ShipmentHeader{WarehouseID: wh, Customer: "Atlas"}, 0)
	s.AddShipmentItem(ctx, sid, id, QtyFromInt(9))

	if _, err := s.AddPick(ctx, sid, 1, id, QtyFromInt(1), 0); !errors.Is(err, ErrWrongWarehouse) {
		t.Fatalf("pick from another warehouse: %v", err)
	}
	if _, err := s.AddPick(ctx, sid, a1, id, QtyFromInt(4), 0); err == nil {
		t.Fatal("A-01 holds only 3")
	}
	res, err := s.AddPick(ctx, sid, a2, id, QtyFromInt(5), 0)
	if err != nil || res.Item.Picked != QtyFromInt(5) || res.Done {
		t.Fatalf("pick = %+v, %v", res, err)
	}
	if _, err := s.AddPick(ctx, sid, a1, id, QtyFromInt(5), 0); !errors.Is(err, ErrOverPick) {
		t.Fatalf("over-pick: %v", err)
	}
	// Load: 5 from the picked A-02, then 3 from A-01, then 1 from GENEL.
	if err := s.LoadShipment(ctx, sid, 0); err != nil {
		t.Fatal(err)
	}
	for bin, want := range map[int64]int64{a1: 0, a2: 0, gen.ID: 4, 1: 100} {
		if q, _ := binQty(ctx, s.DB, bin, id); q != QtyFromInt(want) {
			t.Errorf("bin %d = %v, want %d", bin, q, want)
		}
	}
	// Cancelling returns each unit to the bin it came from.
	if err := s.CancelShipment(ctx, sid, 0); err != nil {
		t.Fatal(err)
	}
	for bin, want := range map[int64]int64{a1: 3, a2: 5, gen.ID: 5} {
		if q, _ := binQty(ctx, s.DB, bin, id); q != QtyFromInt(want) {
			t.Errorf("after cancel bin %d = %v, want %d", bin, q, want)
		}
	}
}

func TestPackages(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := mustProduct(t, s, "P", 0)
	other := mustProduct(t, s, "Q", 0)
	sid, _ := s.CreateShipment(ctx, ShipmentHeader{Customer: "Atlas"}, 0)
	s.AddShipmentItem(ctx, sid, id, QtyFromInt(10))

	k1, _ := s.CreatePackage(ctx, sid, "koli")
	if err := s.PackItem(ctx, k1, other, QtyFromInt(1)); !errors.Is(err, ErrNotOnShipment) {
		t.Fatalf("product not on shipment: %v", err)
	}
	s.PackItem(ctx, k1, id, QtyFromInt(6))
	s.PackItem(ctx, k1, id, QtyFromInt(2))
	if _, err := s.ClosePackage(ctx, k1); err != nil {
		t.Fatal(err)
	}
	k2, _ := s.CreatePackage(ctx, sid, "palet")
	if err := s.PackItem(ctx, k2, id, QtyFromInt(3)); !errors.Is(err, ErrOverPick) {
		t.Fatalf("only 2 left to pack: %v", err)
	}
	empty, _ := s.CreatePackage(ctx, sid, "koli")
	if deleted, _ := s.ClosePackage(ctx, empty); !deleted {
		t.Fatal("empty package should be removed on close")
	}
	pk, _ := s.GetPackage(ctx, k1)
	if pk.Code() != "SVK-"+pk.ShipmentCode[4:]+"-K01" || pk.Units() != QtyFromInt(8) {
		t.Fatalf("package = %s %v", pk.Code(), pk.Units())
	}
	if p2, _ := s.GetPackage(ctx, k2); p2.Code()[len(p2.Code())-3:] != "P02" {
		t.Fatalf("pallet code = %s", p2.Code())
	}
	sh, _ := s.GetShipment(ctx, sid)
	if sh.Items[0].Packed != QtyFromInt(8) {
		t.Fatalf("packed = %v", sh.Items[0].Packed)
	}
}
