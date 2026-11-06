package store

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Qty is a stock quantity stored as thousandths of a unit, so that
// fractional units (kg, m, lt) can be summed without floating point drift.
type Qty int64

const qtyScale = 1000

// ErrInvalidQty is returned when a quantity cannot be parsed.
var ErrInvalidQty = errors.New("geçersiz miktar")

// ParseQty accepts "12", "12,5", "12.5" and "1.250,75" (Turkish grouping).
func ParseQty(s string) (Qty, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return 0, ErrInvalidQty
	}
	switch {
	case strings.Contains(s, ","):
		// Comma is the decimal separator; dots are thousands separators.
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	case strings.Count(s, ".") > 1:
		s = strings.ReplaceAll(s, ".", "")
	case isThousandsDot(s):
		// "1.250" is how Turkish users write one thousand two hundred fifty.
		s = strings.ReplaceAll(s, ".", "")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, ErrInvalidQty
	}
	v := math.Round(f * qtyScale)
	if math.Abs(v) > math.MaxInt64/2 {
		return 0, ErrInvalidQty
	}
	return Qty(v), nil
}

// isThousandsDot reports a single dot followed by exactly three digits with
// a non-zero integer part: "1.250" or "-12.000", but not "0.045" or "1.25".
func isThousandsDot(s string) bool {
	intPart, frac, ok := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if !ok || len(frac) != 3 || intPart == "" || strings.Trim(intPart, "0") == "" {
		return false
	}
	for _, r := range intPart + frac {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// QtyFromInt builds a whole-unit quantity.
func QtyFromInt(n int64) Qty { return Qty(n * qtyScale) }

// Float returns the quantity in units.
func (q Qty) Float() float64 { return float64(q) / qtyScale }

// String formats with Turkish separators: 1.250,5
func (q Qty) String() string {
	neg := q < 0
	if neg {
		q = -q
	}
	whole := int64(q) / qtyScale
	frac := int64(q) % qtyScale

	digits := strconv.FormatInt(whole, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if frac != 0 {
		b.WriteByte(',')
		b.WriteString(strings.TrimRight(fmt.Sprintf("%03d", frac), "0"))
	}
	return b.String()
}

// Input formats the quantity for an <input> value (no grouping).
func (q Qty) Input() string {
	if q%qtyScale == 0 {
		return strconv.FormatInt(int64(q)/qtyScale, 10)
	}
	return strings.Replace(strconv.FormatFloat(q.Float(), 'f', -1, 64), ".", ",", 1)
}
