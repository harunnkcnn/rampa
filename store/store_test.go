package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(context.Background(), CoreMigrations); err != nil {
		t.Fatal(err)
	}
	return s
}

func mustProduct(t *testing.T, s *Store, sku string, min int64) int64 {
	t.Helper()
	id, err := s.CreateProduct(context.Background(), Product{SKU: sku, Name: "Ürün " + sku, MinStock: QtyFromInt(min)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestParseQty(t *testing.T) {
	cases := map[string]Qty{
		"12": 12000, "12,5": 12500, "12.5": 12500, "1.250,75": 1250750, "1.250.000": 1250000000, " 3 ": 3000,
		"1.250": 1250000, "0.045": 45, "12.000": 12000000, "1.2500": 1250,
	}
	for in, want := range cases {
		got, err := ParseQty(in)
		if err != nil || got != want {
			t.Errorf("ParseQty(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1,2,3", "NaN"} {
		if _, err := ParseQty(bad); err == nil {
			t.Errorf("ParseQty(%q) should fail", bad)
		}
	}
	if got := Qty(1250500).String(); got != "1.250,5" {
		t.Errorf("String = %q", got)
	}
	if got := Qty(-3000).String(); got != "-3" {
		t.Errorf("String = %q", got)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Migrate(context.Background(), CoreMigrations); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestDuplicateSKU(t *testing.T) {
	s := newTestStore(t)
	mustProduct(t, s, "A-1", 0)
	_, err := s.CreateProduct(context.Background(), Product{SKU: "a-1", Name: "x"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestMovements(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := mustProduct(t, s, "P1", 5)

	if _, err := s.RecordMovement(ctx, MovementInput{ProductID: id, Kind: MoveIn, Qty: QtyFromInt(10)}); err != nil {
		t.Fatal(err)
	}
	_, err := s.RecordMovement(ctx, MovementInput{ProductID: id, Kind: MoveOut, Qty: QtyFromInt(11)})
	var ise *InsufficientStockError
	if !errors.As(err, &ise) || ise.Available != QtyFromInt(10) {
		t.Fatalf("want InsufficientStockError, got %v", err)
	}
	if _, err := s.RecordMovement(ctx, MovementInput{ProductID: id, Kind: MoveOut, Qty: QtyFromInt(6)}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProduct(ctx, id)
	if p.Stock != QtyFromInt(4) || !p.Low() {
		t.Fatalf("stock = %v low=%v", p.Stock, p.Low())
	}
	// Count says 7: adjust +3.
	if _, err := s.RecordMovement(ctx, MovementInput{ProductID: id, Kind: MoveAdjust, Qty: QtyFromInt(7)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordMovement(ctx, MovementInput{ProductID: id, Kind: MoveAdjust, Qty: QtyFromInt(7)}); !errors.Is(err, ErrNoChange) {
		t.Fatalf("want ErrNoChange, got %v", err)
	}
	p, _ = s.GetProduct(ctx, id)
	if p.Stock != QtyFromInt(7) {
		t.Fatalf("stock after count = %v", p.Stock)
	}
	ms, _ := s.ListMovements(ctx, MovementFilter{ProductID: id})
	if len(ms) != 3 || ms[0].Kind != MoveAdjust || ms[0].Qty != QtyFromInt(3) {
		t.Fatalf("movements = %+v", ms)
	}
	low, _ := s.ListProducts(ctx, ProductFilter{LowOnly: true})
	if len(low) != 0 {
		t.Fatalf("7 > 5 should not be low, got %d", len(low))
	}
}

func TestShipmentLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := mustProduct(t, s, "A", 0)
	b := mustProduct(t, s, "B", 0)
	s.RecordMovement(ctx, MovementInput{ProductID: a, Kind: MoveIn, Qty: QtyFromInt(10)})
	s.RecordMovement(ctx, MovementInput{ProductID: b, Kind: MoveIn, Qty: QtyFromInt(2)})

	sid, err := s.CreateShipment(ctx, ShipmentHeader{Customer: "Atlas Yapı", Plate: "34 abc  123"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadShipment(ctx, sid, 0); !errors.Is(err, ErrEmptyShipment) {
		t.Fatalf("want empty, got %v", err)
	}
	s.AddShipmentItem(ctx, sid, a, QtyFromInt(4))
	s.AddShipmentItem(ctx, sid, a, QtyFromInt(1)) // merges to 5
	s.AddShipmentItem(ctx, sid, b, QtyFromInt(3))

	var ise *InsufficientStockError
	if err := s.LoadShipment(ctx, sid, 0); !errors.As(err, &ise) || ise.SKU != "B" {
		t.Fatalf("want shortage on B, got %v", err)
	}
	pa, _ := s.GetProduct(ctx, a)
	if pa.Stock != QtyFromInt(10) {
		t.Fatalf("failed load must not move stock, A=%v", pa.Stock)
	}

	sh, _ := s.GetShipment(ctx, sid)
	if sh.Plate != "34 ABC 123" || len(sh.Items) != 2 || sh.Items[0].Qty != QtyFromInt(5) {
		t.Fatalf("shipment = %+v", sh)
	}
	if err := s.RemoveShipmentItem(ctx, sid, sh.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadShipment(ctx, sid, 0); err != nil {
		t.Fatal(err)
	}
	pa, _ = s.GetProduct(ctx, a)
	if pa.Stock != QtyFromInt(5) {
		t.Fatalf("A after load = %v", pa.Stock)
	}
	if err := s.AddShipmentItem(ctx, sid, a, QtyFromInt(1)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("loaded shipment must be locked, got %v", err)
	}
	if err := s.CancelShipment(ctx, sid, 0); err != nil {
		t.Fatal(err)
	}
	pa, _ = s.GetProduct(ctx, a)
	if pa.Stock != QtyFromInt(10) {
		t.Fatalf("cancel must return stock, A=%v", pa.Stock)
	}
	if err := s.DeliverShipment(ctx, sid); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancelled cannot be delivered, got %v", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now })
	uid, err := s.CreateUser(ctx, "harun", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "HARUN", "hash", false); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("usernames are case-insensitive, got %v", err)
	}
	s.CreateSession(ctx, "tok", "csrf", uid, now.Add(time.Hour))
	if ss, err := s.GetSession(ctx, "tok"); err != nil || ss.User.Username != "harun" {
		t.Fatalf("session = %+v, %v", ss, err)
	}
	s.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	if _, err := s.GetSession(ctx, "tok"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session should be gone, got %v", err)
	}
}
