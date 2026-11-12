# El terminali kurulumu

Rampa'nın terminal ekranları (`/t`) bir web sayfasıdır. Terminale uygulama kurulmaz; tarayıcı yeterlidir. Bu sayede aynı ekranlar Windows ve Android çalışan el terminallerinde, hatta telefonlarda çalışır.

## 1. Sunucuyu ağa açın

Rampa'yı depodaki bir bilgisayarda (ya da sunucuda) ağdan erişilecek şekilde başlatın:

```bash
rampa -addr 0.0.0.0:8080 -db rampa.db
```

Terminaller aynı kablosuz ağdan `http://BILGISAYARIN-IPSI:8080/t` adresine bağlanır. Bilgisayarın IP adresini sabitleyin (modemden DHCP rezervasyonu), yoksa adres değişince terminaller bağlanamaz. Windows güvenlik duvarında 8080 portuna gelen bağlantıya izin verin.

İnternete açık bir sunucuda çalıştıracaksanız önüne HTTPS sağlayan bir ters vekil (Caddy, Nginx, IIS) koyun ve `-secure-cookie` ile başlatın.

## 2. Kullanıcı, PIN ve depo yetkisi

Panelde **Ayarlar → Kullanıcılar**:

1. Her personele bir kullanıcı açın. Kullanıcı adı sicil numarası olabilir; terminalin sayı tuşlarıyla yazmak kolaylaşır.
2. **Terminal PIN** kutusuna 4–8 haneli bir PIN girip **Ver**'e basın.
3. Personelin çalışacağı depoları işaretleyin. Tek depoya yetkili kullanıcı, girişte depo seçmeden doğrudan menüye düşer.

PIN yalnızca terminal ekranlarını açar. Aynı kişi yönetim paneline parolasıyla girer.

## 3. Hücre etiketleri

**Depolar ve hücreler → Hücreler → Etiketleri yazdır** her hücre için 100×60 mm, Code128 barkodlu bir etiket sayfası açar. Etiketi raf gözüne yapıştırın; terminalde hücre kutusuna bu barkod okutulur.

Ürünlerin kendi barkodu (EAN vb.) varsa ürün kartındaki **Barkod** alanına girin. Barkodu olmayan ürünlerde stok kodu okutulur ya da yazılır.

## 4. Okuyucu ayarı (en önemli adım)

Terminalin barkod okuyucusu **klavye öykünmesi** (keyboard wedge / "klavye çıkışı") modunda olmalı ve her okumadan sonra **Enter** göndermeli. Rampa'nın "okut, sonraki kutuya geç, kaydet" akışı buna dayanır.

Newland terminallerde bu ayar okuyucu ayar uygulamasındadır (Android'de genellikle *Scan Manager* / *Scanner Settings*, Windows'ta üreticinin okuyucu yardımcı programı). Aranacak seçenekler:

| Ayar | Değer |
|---|---|
| Çıkış modu | Klavye / Keyboard emulation (Broadcast/Intent değil) |
| Son ek (suffix) | Enter (CR veya CR+LF) |
| Ön ek (prefix) | Yok |
| Klavye dili | Terminalin klavye düzeniyle aynı |

Ayar menülerinin adı modele ve yazılım sürümüne göre değişir; emin değilseniz cihazın kılavuzuna bakın.

**Kontrol:** Terminalde not defteri ya da herhangi bir metin kutusu açıp bir barkod okutun. Kod eksiksiz yazılıyor ve imleç alt satıra iniyorsa ayar doğrudur. Karakterler yanlış çıkıyorsa (ör. `-` yerine başka bir işaret) okuyucunun klavye dili ile terminalin klavye düzeni farklıdır.

## 5. Tarayıcı

- **Android:** Chrome. Adresi açıp menüden *Ana ekrana ekle* deyin; Rampa tam ekran bir uygulama gibi açılır.
- **Windows:** Edge (Chromium) ya da Chrome önerilir. Sayfalar Internet Explorer 11'de de çalışacak şekilde yazıldı, ancak IE desteği Microsoft tarafından sona erdi; mümkünse Edge kurun.
- Tarayıcının F1 tuşunu yardım için kullanması Rampa'nın F1 = Geri kısayolunu engelleyebilir. Bu durumda alt çubuktaki **Geri**'ye dokunun.

## Ekranlar ve tuşlar

| Ekran | Akış | Tuşlar |
|---|---|---|
| Mal kabul | Tedarikçi + irsaliye → hücre okut → ürün okut (miktar boşsa 1) | F2 son okutmayı geri al, F4 bitir |
| Sevkiyat toplama | Sevkiyat seç → hücre okut → ürün okut. Liste ürünün hangi hücrede olduğunu gösterir | F4 yükle ve stoktan düş |
| Sayım | Hücre okut → raftaki her ürünü okut (düzeltme için miktara -1) → bitir, farkları onayla | F3 iptal, F4 bitir |
| Hücre takibi | Hücre okutunca içindekiler, ürün okutunca bulunduğu hücreler | F2 transfer |
| Transfer | Kaynak hücre → ürün → miktar → hedef hücre | |
| Paketleme | Sevkiyat seç → yeni koli/palet → ürün okut → kapat. Etiket panelden yazdırılır | F2 koli, F3 palet, F4 kapat |

Her ekranda F1 geri döner; listelerde 1–9 tuşları satırı açar.
