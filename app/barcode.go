package app

import (
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// code128 holds the bar/space widths of every Code 128 symbol; 106 is stop.
var code128 = [...]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

const (
	code128StartB = 104
	code128Stop   = 106
)

var errNotEncodable = errors.New("barkoda yalnızca ASCII karakterler yazılabilir")

// Code128Modules encodes text in code set B and returns alternating bar and
// space widths, starting with a bar, without quiet zones.
func Code128Modules(text string) ([]int, error) {
	if text == "" {
		return nil, errNotEncodable
	}
	symbols := []int{code128StartB}
	sum := code128StartB
	for i, r := range text {
		if r < 32 || r > 126 {
			return nil, errNotEncodable
		}
		v := int(r) - 32
		symbols = append(symbols, v)
		sum += v * (i + 1)
	}
	symbols = append(symbols, sum%103, code128Stop)
	var widths []int
	for _, sym := range symbols {
		for _, c := range code128[sym] {
			widths = append(widths, int(c-'0'))
		}
	}
	return widths, nil
}

// Code128SVG draws a scannable barcode. Height is in modules; the drawing
// scales to its container, so print size is set with CSS.
func Code128SVG(text string, height int) template.HTML {
	widths, err := Code128Modules(text)
	if err != nil {
		return template.HTML(`<span class="barcode-missing">` + template.HTMLEscapeString(text) + `</span>`)
	}
	const quiet = 10
	total := quiet * 2
	for _, w := range widths {
		total += w
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="barcode" viewBox="0 0 %d %d" preserveAspectRatio="none" role="img" aria-label="Barkod %s" shape-rendering="crispEdges">`,
		total, height, template.HTMLEscapeString(text))
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/>`, total, height)
	x := quiet
	for i, w := range widths {
		if i%2 == 0 {
			fmt.Fprintf(&b, `<rect x="%d" width="%d" height="%d" fill="#000"/>`, x, w, height)
		}
		x += w
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
