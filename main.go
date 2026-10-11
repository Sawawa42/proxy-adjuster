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
	"strconv"
	"strings"

	"codeberg.org/go-pdf/fpdf"
	_ "golang.org/x/image/webp"
)

const (
	a4Short = 210.0
	a4Long  = 297.0
)

type inputSpec struct {
	path  string
	count int
}

type inputList []inputSpec

func (l *inputList) String() string {
	s := make([]string, len(*l))
	for i, in := range *l {
		s[i] = fmt.Sprintf("%s:%d", in.path, in.count)
	}
	return strings.Join(s, ",")
}

// Set は "path:N" 形式を受け取る。末尾の ":N" が整数でなければ値全体をパスとみなし、
// 枚数は1とする(Windows の "C:\..." などを誤判定しないため)。
func (l *inputList) Set(v string) error {
	spec := inputSpec{path: v, count: 1}
	if i := strings.LastIndex(v, ":"); i >= 0 {
		if n, err := strconv.Atoi(v[i+1:]); err == nil {
			if n < 1 {
				return fmt.Errorf("%s: 枚数は1以上を指定してください", v)
			}
			spec = inputSpec{path: v[:i], count: n}
		}
	}
	*l = append(*l, spec)
	return nil
}

type layout struct {
	orientation      string // "P" or "L"
	pageW, pageH     float64
	cols, rows       int
	marginX, marginY float64
}

func (lo layout) perPage() int { return lo.cols * lo.rows }

// fit は n*card + (n-1)*gap <= page を満たす最大の n を返す。
func fit(page, card, gap float64) int {
	// 浮動小数点誤差でぴったりのサイズが1枚減らないように微小値を足す
	return int(math.Floor((page+gap)/(card+gap) + 1e-9))
}

// computeLayout は縦向き・横向きのうち1ページに多く入る方を選び、
// 余白を上下左右に均等に振り分けてグリッドを中央に配置する。
func computeLayout(cardW, cardH, gap float64) (layout, error) {
	candidates := []layout{
		{orientation: "P", pageW: a4Short, pageH: a4Long},
		{orientation: "L", pageW: a4Long, pageH: a4Short},
	}
	best := layout{}
	for _, c := range candidates {
		c.cols = fit(c.pageW, cardW, gap)
		c.rows = fit(c.pageH, cardH, gap)
		if c.perPage() > best.perPage() {
			best = c
		}
	}
	if best.perPage() == 0 {
		return layout{}, fmt.Errorf("%gmm x %gmm の画像はA4に収まりません", cardH, cardW)
	}
	best.marginX = (best.pageW - float64(best.cols)*cardW - float64(best.cols-1)*gap) / 2
	best.marginY = (best.pageH - float64(best.rows)*cardH - float64(best.rows-1)*gap) / 2
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

func run(inputs []inputSpec, output string, cardW, cardH, gap float64) error {
	lo, err := computeLayout(cardW, cardH, gap)
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

	// 同じ画像はPDFに1回だけ埋め込み、配置だけを枚数分くり返す
	names := map[string]string{}
	placed := 0
	for _, in := range inputs {
		name, ok := names[in.path]
		if !ok {
			data, imageType, err := loadImage(in.path)
			if err != nil {
				return err
			}
			name = fmt.Sprintf("img%d", len(names))
			pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: imageType}, bytes.NewReader(data))
			if pdf.Err() {
				return fmt.Errorf("%s: %w", in.path, pdf.Error())
			}
			names[in.path] = name
		}

		for range in.count {
			slot := placed % lo.perPage()
			if slot == 0 {
				pdf.AddPage()
			}
			x := lo.marginX + float64(slot%lo.cols)*(cardW+gap)
			y := lo.marginY + float64(slot/lo.cols)*(cardH+gap)
			pdf.ImageOptions(name, x, y, cardW, cardH, false, fpdf.ImageOptions{}, 0, "")
			placed++
		}
	}
	if pdf.Err() {
		return pdf.Error()
	}

	if err := pdf.OutputFileAndClose(output); err != nil {
		return err
	}

	pages := (placed + lo.perPage() - 1) / lo.perPage()
	fmt.Printf("%s: A4%s %d列 x %d行 (余白 左右%.1fmm 上下%.1fmm, 間隔%.1fmm), %d枚 / %dページ\n",
		output, map[string]string{"P": "縦", "L": "横"}[lo.orientation],
		lo.cols, lo.rows, lo.marginX, lo.marginY, gap, placed, pages)
	return nil
}

func main() {
	var inputs inputList
	fs := flag.NewFlagSet("proxy-adjuster", flag.ExitOnError)
	fs.Var(&inputs, "i", "入力画像のパス(複数指定可)。`path:N` で N 枚配置する")
	output := fs.String("o", "", "出力PDFのパス")
	cardH := fs.Float64("l", 0, "画像の縦サイズ(mm)")
	cardW := fs.Float64("w", 0, "画像の横サイズ(mm)")
	gap := fs.Float64("g", 0, "画像同士の間隔(mm)。切り取り用の空白")

	// `-i *.jpg -o out.pdf` のように -i の後ろに複数ファイルが並ぶ形も受け付けるため、
	// フラグ以外の引数は入力画像として扱い、残りのフラグの解析を続ける。
	args := os.Args[1:]
	for {
		fs.Parse(args)
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		if err := inputs.Set(rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
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
	if *gap < 0 {
		errs = append(errs, errors.New("-g には0以上の値(mm)を指定してください"))
	}
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, errors.Join(errs...))
		fs.Usage()
		os.Exit(2)
	}

	if err := run(inputs, *output, *cardW, *cardH, *gap); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
