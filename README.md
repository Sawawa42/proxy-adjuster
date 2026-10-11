# proxy-adjuster

縦 Xmm x 横 Ymmの画像を、A4に収まるように最大限敷き詰め、PDFに変換するコマンドラインツール

## 使い方

```bash
proxy-adjuster -i image/* -o out.pdf -l 88 -w 63
# -i: 入力画像のパス(複数選択可)
# -o: 出力PDFのパス
# -l: 画像の縦サイズ(mm)
# -w: 画像の横サイズ(mm)
# -g: 画像間の余白サイズ(mm, float, default: 0)

# hoge.webpをA4のページあたり9枚、2ページに画像同士の余白0.1mmで敷き詰めてPDFに変換する場合
proxy-adjuster -i hoge.webp:18 -o output.pdf -l 88 -w 63 -g 0.1
```