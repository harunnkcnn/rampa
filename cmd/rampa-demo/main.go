// Command rampa-demo fills a database with realistic sample data for trying
// Rampa out. It refuses to touch a database that already has products.
//
//	go run ./cmd/rampa-demo -db demo.db
//	go run ./cmd/rampa -db demo.db   # sign in: demo / demo-parola
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/harunnkcnn/rampa/app"
	"github.com/harunnkcnn/rampa/store"
)

type seedProduct struct {
	sku, name, unit, loc string
	min                  int64
	kg, m3               float64
	daily                float64 // average units shipped per day
}

var catalog = []seedProduct{
	{"FLT-1042", "Yağ filtresi 1042", "adet", "A-01-1", 40, 0.45, 0.0021, 4},
	{"FLT-2210", "Hava filtresi 2210", "adet", "A-01-2", 30, 0.8, 0.0085, 2.5},
	{"BRK-0310", "Fren balatası ön takım", "takım", "A-02-1", 25, 2.1, 0.004, 3},
	{"BRK-0420", "Fren diski 280 mm", "adet", "A-02-3", 16, 6.4, 0.0065, 1.2},
	{"AKU-72", "Akü 72 Ah", "adet", "B-01-1", 10, 18.5, 0.011, 0.8},
	{"YAG-5W30", "Motor yağı 5W-30 4 lt", "bidon", "B-02-1", 60, 3.7, 0.0049, 6},
	{"ANT-01", "Antifriz 3 lt", "bidon", "B-02-2", 30, 3.3, 0.0041, 2},
	{"SLB-25", "Silecek süpürgesi 55 cm", "adet", "C-01-4", 20, 0.3, 0.0015, 1.5},
	{"AMR-110", "Amortisör arka", "adet", "C-03-1", 8, 3.9, 0.012, 0.6},
	{"BUJ-4", "Buji iridyum", "adet", "C-01-1", 80, 0.05, 0.0001, 7},
	{"TRG-90", "Triger seti", "set", "C-02-2", 6, 2.8, 0.009, 0.4},
	{"KAY-60", "V kayışı 6PK", "adet", "C-02-4", 15, 0.25, 0.0012, 1},
}

var customers = []string{"Atlas Oto Servis", "Kuzey Yedek Parça", "Marmara Filo", "Ege Otomotiv", "Bursa Lastik ve Bakım"}
var plates = []string{"34 KLM 482", "16 BDR 115", "35 EGE 903", "41 TRS 207"}

func main() {
	dbPath := flag.String("db", "demo.db", "database file")
	flag.Parse()
	if err := run(*dbPath); err != nil {
		fmt.Fprintln(os.Stderr, "rampa-demo:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	ctx := context.Background()
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx, store.CoreMigrations); err != nil {
		return err
	}
	if ps, _ := st.ListProducts(ctx, store.ProductFilter{IncludeArchived: true}); len(ps) > 0 {
		return fmt.Errorf("%s zaten veri içeriyor; boş bir dosya seçin", path)
	}

	uid, err := st.CreateUser(ctx, "demo", mustHash("demo-parola"), true)
	if err != nil {
		return err
	}
	_ = st.SetSetting(ctx, "company_name", "Örnek Yedek Parça Deposu")

	rng := rand.New(rand.NewPCG(42, 7))
	now := time.Now()
	y, m, d := now.AddDate(0, 0, -45).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	clock := start
	st.SetClock(func() time.Time { return clock })

	ids := make([]int64, len(catalog))
	for i, c := range catalog {
		ids[i], err = st.CreateProduct(ctx, store.Product{
			SKU: c.sku, Name: c.name, Unit: c.unit, Location: c.loc,
			MinStock: store.QtyFromInt(c.min), WeightKg: c.kg, VolumeM3: c.m3,
		})
		if err != nil {
			return err
		}
		opening := int64(c.daily*20) + c.min
		if _, err := st.RecordMovement(ctx, store.MovementInput{
			ProductID: ids[i], Kind: store.MoveIn, Qty: store.QtyFromInt(opening), Note: "Açılış stoğu", UserID: uid,
		}); err != nil {
			return err
		}
	}

	for day := 1; day <= 45; day++ {
		clock = start.AddDate(0, 0, day).Add(8*time.Hour + 30*time.Minute)
		if clock.After(now) {
			break
		}
		// One or two shipments a day.
		for n := 0; n < 1+rng.IntN(2); n++ {
			clock = clock.Add(time.Duration(30+rng.IntN(120)) * time.Minute)
			sid, err := st.CreateShipment(ctx, store.ShipmentHeader{
				Customer: customers[rng.IntN(len(customers))], Plate: plates[rng.IntN(len(plates))], Driver: "",
			}, uid)
			if err != nil {
				return err
			}
			for _, i := range rng.Perm(len(catalog))[:2+rng.IntN(3)] {
				q := int64(catalog[i].daily*float64(1+rng.IntN(3))) + 1
				_ = st.AddShipmentItem(ctx, sid, ids[i], store.QtyFromInt(q))
			}
			if day >= 44 && n == 0 {
				continue // leave a couple of drafts open
			}
			if err := st.LoadShipment(ctx, sid, uid); err != nil {
				_ = st.CancelShipment(ctx, sid, uid)
				continue
			}
			if day < 43 {
				_ = st.DeliverShipment(ctx, sid)
			}
		}
		// Weekly restock of whatever is low.
		if day%7 == 0 {
			clock = clock.Add(2 * time.Hour)
			low, _ := st.ListProducts(ctx, store.ProductFilter{LowOnly: true})
			for _, p := range low {
				if rng.IntN(3) == 0 {
					continue // some stay low on purpose
				}
				_, _ = st.RecordMovement(ctx, store.MovementInput{
					ProductID: p.ID, Kind: store.MoveIn, Qty: p.MinStock * 3, Note: "Haftalık tedarik", UserID: uid,
				})
			}
		}
	}
	fmt.Printf("Örnek veri hazır: %s\nGiriş: demo / demo-parola\n", path)
	return nil
}

func mustHash(pw string) string {
	h, err := app.HashPassword(pw)
	if err != nil {
		panic(err)
	}
	return h
}
