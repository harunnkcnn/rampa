# Katkıda bulunma

Rampa'ya katkı vermek istediğiniz için teşekkürler.

## Hata bildirimi ve öneri

[Issues](https://github.com/harunnkcnn/rampa/issues) bölümünden hazır şablonlardan birini seçin. Hata bildirirken hangi ekranda olduğunuzu, ne yaptığınızı ve ne beklediğinizi yazın.

## Kod katkısı

1. Repoyu fork'layın ve yeni bir dal açın: `git checkout -b ozellik/kisa-aciklama`
2. Bağımlılıkları kurun: `npm install`
3. Değişikliğinizi yapın. İş kuralı değiştiyse `tests/` altına test ekleyin.
4. Göndermeden önce hepsinin geçtiğinden emin olun:

   ```bash
   npm run check   # tip kontrolü + testler + derleme
   ```

5. Pull request açın ve neyi neden değiştirdiğinizi kısaca anlatın.

## Kod düzeni

- İş kuralları `src/domain` içinde, arayüzden bağımsız saf fonksiyonlar olarak yazılır ve `Result<T>` döndürür.
- Bileşenler `src/components` altındadır; stil için Tailwind sınıfları kullanılır.
- Arayüz metinleri Türkçedir.
