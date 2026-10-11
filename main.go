package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"

	"codeberg.org/go-pdf/fpdf"
	_ "golang.org/x/image/webp"
)

const (
	a4Short = 210.0
	a4Long  = 297.0
)

type inputList []string

func (l *inputList) String() string { return strings.Join(*l, ",") }

func (l *inputList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

type layout struct {
	orientation      string // "P" or "L"
	pageW, pageH     float64
	cols, rows       int
	marginX, marginY float64
}

func (lo layout) perPage() int { return lo.cols * lo.rows }

// computeLayout は縦向き・横向きのうち1ページに多く入る方を選び、
// 余白を上下左右に均等に振り分けてグリッドを中央に配置する。
func computeLayout(cardW, cardH float64) (layout, error) {
	candidates := []layout{
		{orientation: "P", pageW: a4Short, pageH: a4Long},
		{orientation: "L", pageW: a4Long, pageH: a4Short},
	}
	best := layout{}
	for _, c := range candidates {
		// 浮動小数点誤差でぴったりのサイズが1枚減らないように微小値を足す
		c.cols = int(math.Floor(c.pageW/cardW + 1e-9))
		c.rows = int(math.Floor(c.pageH/cardH + 1e-9))
		if c.perPage() > best.perPage() {
			best = c
		}
	}
	if best.perPage() == 0 {
		return layout{}, fmt.Errorf("%gmm x %gmm の画像はA4に収まりません", cardH, cardW)
	}
	best.marginX = (best.pageW - float64(best.cols)*cardW) / 2
	best.marginY = (best.pageH - float64(best.rows)*cardH) / 2
	return best, nil
}

// loadImage は fpdf に渡せる形式で画像を読み込む。
// JPEG はそのまま埋め込み、それ以外(WebP, PNG, GIF)は 8bit の非インターレース PNG に変換する。
func loadImage(path string) (data []byte, imageType string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%s: 未対応の画像形式です: %w", path, err)
	}
	if format == "jpeg" {
		return raw, "JPG", nil
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	dst := image.NewNRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return buf.Bytes(), "PNG", nil
}

func run(inputs []string, output string, cardW, cardH float64) error {
	lo, err := computeLayout(cardW, cardH)
	if err != nil {
		return err
	}

	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: lo.orientation,
		UnitStr:        "mm",
		SizeStr:        "A4",
	})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)

	for i, path := range inputs {
		data, imageType, err := loadImage(path)
		if err != nil {
			return err
		}

		slot := i % lo.perPage()
		if slot == 0 {
			pdf.AddPage()
		}
		x := lo.marginX + float64(slot%lo.cols)*cardW
		y := lo.marginY + float64(slot/lo.cols)*cardH

		name := fmt.Sprintf("img%d", i)
		opt := fpdf.ImageOptions{ImageType: imageType}
		pdf.RegisterImageOptionsReader(name, opt, bytes.NewReader(data))
		pdf.ImageOptions(name, x, y, cardW, cardH, false, opt, 0, "")
		if pdf.Err() {
			return fmt.Errorf("%s: %w", path, pdf.Error())
		}
	}

	if err := pdf.OutputFileAndClose(output); err != nil {
		return err
	}

	pages := (len(inputs) + lo.perPage() - 1) / lo.perPage()
	fmt.Printf("%s: A4%s %d列 x %d行 (余白 左右%.1fmm 上下%.1fmm), %d枚 / %dページ\n",
		output, map[string]string{"P": "縦", "L": "横"}[lo.orientation],
		lo.cols, lo.rows, lo.marginX, lo.marginY, len(inputs), pages)
	return nil
}

func main() {
	var inputs inputList
	fs := flag.NewFlagSet("proxy-adjuster", flag.ExitOnError)
	fs.Var(&inputs, "i", "入力画像のパス(複数指定可)")
	output := fs.String("o", "", "出力PDFのパス")
	cardH := fs.Float64("l", 0, "画像の縦サイズ(mm)")
	cardW := fs.Float64("w", 0, "画像の横サイズ(mm)")

	// `-i *.jpg -o out.pdf` のように -i の後ろに複数ファイルが並ぶ形も受け付けるため、
	// フラグ以外の引数は入力画像として扱い、残りのフラグの解析を続ける。
	args := os.Args[1:]
	for {
		fs.Parse(args)
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		inputs = append(inputs, rest[0])
		args = rest[1:]
	}

	var errs []error
	if len(inputs) == 0 {
		errs = append(errs, errors.New("-i で入力画像を指定してください"))
	}
	if *output == "" {
		errs = append(errs, errors.New("-o で出力PDFのパスを指定してください"))
	}
	if *cardH <= 0 || *cardW <= 0 {
		errs = append(errs, errors.New("-l と -w に正のサイズ(mm)を指定してください"))
	}
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, errors.Join(errs...))
		fs.Usage()
		os.Exit(2)
	}

	if err := run(inputs, *output, *cardW, *cardH); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
