package app

import (
	"strings"
	"testing"
)

// modulesString expands widths into the 1/0 module string reference
// encoders print (bars are 1).
func modulesString(widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		c := "1"
		if i%2 == 1 {
			c = "0"
		}
		b.WriteString(strings.Repeat(c, w))
	}
	return b.String()
}

func TestCode128Table(t *testing.T) {
	for i, p := range code128 {
		sum := 0
		for _, c := range p {
			sum += int(c - '0')
		}
		want := 11
		if i == code128Stop {
			want = 13
		}
		if sum != want {
			t.Errorf("symbol %d %s sums to %d", i, p, sum)
		}
	}
}

func TestCode128Reference(t *testing.T) {
	// Expected strings come from python-barcode's Code128 for the same input.
	for text, want := range code128Fixtures {
		w, err := Code128Modules(text)
		if err != nil {
			t.Fatal(err)
		}
		if got := modulesString(w); got != want {
			t.Errorf("%s:\n got  %s\n want %s", text, got, want)
		}
	}
	if _, err := Code128Modules("ŞUBE"); err == nil {
		t.Error("non-ASCII must be refused")
	}
}

// Strings without long digit runs, where the reference encoder also stays in
// code set B.
var code128Fixtures = map[string]string{
	"A-01-1":      "11010010000101000110001001101110010011101100100111001101001101110010011100110100010110001100011101011",
	"Hello world": "110100100001100010100010110010000110010100001100101000010001111010110110011001111001010010001111010100100111101100101000010000100110110010000101100011101011",
}
