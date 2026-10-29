<h1 align="center">🚚 Rampa</h1>

<p align="center">
  <b>Stock, document-based stock in/out and shipment tracking for auto parts warehouses.</b><br>
  No install, no server. A single HTML file that opens in your browser.
</p>

<p align="center">
  <a href="https://harunnkcnn.github.io/rampa/"><b>▶ Live demo</b></a> ·
  <a href="https://github.com/harunnkcnn/rampa/releases/latest"><b>⬇ Download</b></a> ·
  <a href="README.md">Türkçe</a>
</p>

<p align="center">
  <img src="docs/stok.png" width="260" alt="Stock screen">
  <img src="docs/hareket.png" width="260" alt="Stock in/out screen">
  <img src="docs/sevkiyat.png" width="260" alt="Shipment screen">
</p>

## Why?

In many warehouses a part's story is written in three places: goods arrive with a paper waybill, stock lives in a spreadsheet, and the location of a shipment is found out by phone. Information gets delayed or lost at every hand-off. Rampa brings those three steps into one screen. It was designed from what a dispatch worker sees on the loading dock.

## Features

- **Stock:** parts with OEM code, name and shelf address (e.g. `A-03-2`). Critical stock surfaces automatically, shown with both colour and text.
- **Stock in/out:** every movement requires a document number (waybill or work order). Parts that are not in stock cannot be issued.
- **Shipments:** *Preparing → Loaded → On the road → Delivered*. Stock is deducted once, at the *Loaded* step.
- **Loading list:** one tap to share the list with the driver, or copy it to the clipboard.
- **Backup:** export and restore all data as JSON.
- **Built for the floor:** large buttons in the lower half of the screen for one-handed use, monospaced part codes, light and dark themes.

## Getting started

Open the [live demo](https://harunnkcnn.github.io/rampa/), or download `rampa.html` from the [latest release](https://github.com/harunnkcnn/rampa/releases/latest) and open it in any browser. Data stays in that browser's local storage; use **Ayarlar → Yedek indir** for backups. The interface is in Turkish.

## Development

```bash
npm install
npm run dev        # dev server
npm test           # domain tests
npm run typecheck  # TypeScript
npm run build      # dist/index.html (single file)
```

React 19 · TypeScript (strict) · Tailwind CSS 4 · Vite 6 (`vite-plugin-singlefile`) · Node test runner · GitHub Actions (typecheck, tests, build, GitHub Pages, releases).

Business rules live in `src/domain` as pure functions that return a `Result<T>`, so they are easy to test and independent of the UI.

## Rampa Pro

Adds a 7-day report, reorder suggestions for critical stock and a vehicle load plan, under a commercial license with a 14-day trial. Contact: [harunnkcnn@gmail.com](mailto:harunnkcnn@gmail.com?subject=Rampa%20Pro).

## License

[MIT](LICENSE) © 2026 Harun Kaçan
