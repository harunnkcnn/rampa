# Değişiklik günlüğü

Bu projedeki önemli değişiklikler bu dosyada tutulur. Sürümler [Semantic Versioning](https://semver.org/lang/tr/) kurallarına uyar.

## [1.0.0] - 2026-10-29

### Eklendi
- Stok: OEM/parça kodu, ad ve raf adresiyle parça kaydı; Tükendi, Kritik, Düşük, Yeterli durumları
- Giriş/çıkış: belge numarası zorunlu hareket kaydı; stokta olmayan parça çıkarılamaz
- Sevkiyat: Hazırlanıyor → Yüklendi → Yolda → Teslim edildi akışı; "Yüklendi" adımında stoktan otomatik düşüm
- Tek dokunuşla paylaşılabilen yükleme listesi
- JSON yedek indirme ve geri yükleme
- Açık ve koyu tema, telefonda tek elle kullanıma uygun arayüz
- React 19, TypeScript, Tailwind CSS 4 ve Vite ile tek HTML dosyası çıktısı
- Domain testleri ve GitHub Actions ile otomatik test, derleme, canlı demo ve sürüm yayını

[1.0.0]: https://github.com/harunnkcnn/rampa/releases/tag/v1.0.0
