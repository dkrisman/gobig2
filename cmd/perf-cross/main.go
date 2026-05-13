// Command perf-cross times each installed JBIG2 decoder
// (gobig2, jbig2dec, mutool, pdfimages, PDFBox) over a fixture
// set and emits a Markdown comparison table plus the raw JSON
// measurement matrix. Dev / CI tool driven by `task bench:cross`
// and .github/workflows/perf-linux.yml.
//
// # Fixtures
//
// Two sources, both materialized as [fixture] with absolute
// paths:
//
//   - Built-in: bundled SerenityOS subset.
//     testdata/serenityos/<name>.jbig2 (standalone) and
//     testdata/pdf-embedded/serenityos/<name>-obj5.jb2 (segment
//     stream for PDF wrapping). One entry per major codec path.
//
//   - -extra-corpus-dir: larger synthesized fixtures.
//     <name>.jb2 + <name>.txt + optional <name>-embedded.jb2.
//     Without the embedded form, PDF decoders skip that fixture.
//
// Dimensions live in the <name>.txt sidecar; the PDF wrapper
// needs them for /Width / /Height.
//
// # Run
//
// Per (decoder, fixture) cell: cfg.warmup discarded subprocess
// invocations followed by cfg.iters timed ones; cell value is
// the best (min) wall-clock duration in ms.
//
// # Output format
//
// -output-format chooses the per-decoder file format every
// invocation writes:
//
//   - pbm (default): raw bit-packed Netpbm. All decoders use
//     their native bilevel path (gobig2 --format=pbm, jbig2dec
//     -t pbm, mutool draw -o *.pbm, pdfimages without -png).
//     Output is near-zero cost vs decode, so cell values track
//     decode work. PDFBox skips - Java ImageIO has no PBM
//     writer.
//
//   - png: every decoder writes PNG. Includes encode overhead
//     in the cell value (Go image/png on gobig2 vs libpng on
//     jbig2dec; the gap is substantial on bilevel content). Use
//     when PDFBox is in the decoder set.
//
// # Output
//
// -out-md writes the Markdown table (or stdout for "-").
// -out-json writes the measurement matrix. CI feeds the Markdown
// into $GITHUB_STEP_SUMMARY and uploads the JSON as an artifact.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fixture pairs a bench name with absolute paths for each
// consumer family. StandalonePath is the T.88 Annex E form
// (gobig2, jbig2dec); EmbeddedPath is the headerless segment
// stream wrapped into a PDF for the PDF decoders. Empty
// EmbeddedPath -> PDF decoders skip this fixture. Width / Height
// feed the PDF wrapper's /MediaBox and image dict.
type fixture struct {
	Name           string
	StandalonePath string
	EmbeddedPath   string // optional; "" -> PDF decoders skip
	Width          int
	Height         int
}

// builtinFixtureSpec describes the in-tree fixture list, joined
// against cfg.standaloneDir / cfg.embeddedDir at runtime. Mirrors
// the bench_test.go entry list so cross-decoder timings line up
// with the in-process benches.
type builtinFixtureSpec struct {
	name           string
	standaloneFile string
	embeddedFile   string
	sidecarFile    string
}

var builtinFixtureSpecs = []builtinFixtureSpec{
	{"bitmap", "bitmap.jbig2", "bitmap-obj5.jb2", "bitmap-obj5.txt"},
	{"bitmap-mmr", "bitmap-mmr.jbig2", "bitmap-mmr-obj5.jb2", "bitmap-mmr-obj5.txt"},
	{"bitmap-halftone", "bitmap-halftone.jbig2", "bitmap-halftone-obj5.jb2", "bitmap-halftone-obj5.txt"},
	{"bitmap-symbol", "bitmap-symbol.jbig2", "bitmap-symbol-obj5.jb2", "bitmap-symbol-obj5.txt"},
	{"bitmap-symbol-symhuff-texthuff", "bitmap-symbol-symhuff-texthuff.jbig2", "bitmap-symbol-symhuff-texthuff-obj5.jb2", "bitmap-symbol-symhuff-texthuff-obj5.txt"},
}

// decoderKind selects which fixture form the runner hands to
// the decoder.
type decoderKind int

const (
	kindStandalone decoderKind = iota // raw .jbig2 file
	kindPDF                           // PDF-wrapped fixture
)

// decoder is one comparison target. Available reports whether
// the decoder is invocable (or returns a skip reason);
// BuildCommand returns the subprocess for one iter.
type decoder struct {
	Name         string
	Kind         decoderKind
	BinaryProbe  string // PATH lookup name; empty when the binary path is a flag
	BuildCommand func(input, outDir string) (*exec.Cmd, error)
	Available    func(cfg *config) (bool, string)
}

// config aggregates the flag set. Some decoders need explicit
// paths (gobig2 binary, pdfbox jar) that the runner can't infer
// at build time.
type config struct {
	standaloneDir  string
	embeddedDir    string
	extraCorpusDir string // synthesized fixtures dropped here by CI / scripts/perf/synthesize-corpus.sh
	gobig2Bin      string
	pdfboxJar      string
	javaBin        string
	tmpDir         string
	iters          int
	warmup         int
	timeout        time.Duration
	skipBuiltin    bool   // when true, only -extra-corpus-dir fixtures run
	outputFormat   string // "pbm" (default) or "png"; selects per-decoder native invocation
	decoders       []string
	outMD          string
	outJSON        string
	gobig2Version  string // populated at runtime, embedded in report header
}

// measurement is one (decoder, fixture) cell. BestMs is the min
// over cfg.iters timed runs. Skipped marks unavailable decoders
// or fixtures lacking the required form.
type measurement struct {
	Decoder    string  `json:"decoder"`
	Fixture    string  `json:"fixture"`
	Iters      int     `json:"iters"`
	BestMs     float64 `json:"best_ms,omitempty"`
	MeanMs     float64 `json:"mean_ms,omitempty"`
	MedianMs   float64 `json:"median_ms,omitempty"`
	OK         bool    `json:"ok"`
	Skipped    bool    `json:"skipped,omitempty"`
	SkipReason string  `json:"skip_reason,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// report is the JSON artifact uploaded by CI. Decoders /
// Fixtures preserve the column / row order the Markdown table
// uses.
type report struct {
	Header       string        `json:"header"`
	GeneratedAt  string        `json:"generated_at"`
	GoVersion    string        `json:"go_version"`
	GOOS         string        `json:"goos"`
	GOARCH       string        `json:"goarch"`
	GobigVersion string        `json:"gobig2_version"`
	OutputFormat string        `json:"output_format"`
	Iters        int           `json:"iters"`
	Warmup       int           `json:"warmup"`
	Decoders     []string      `json:"decoders"`
	Fixtures     []string      `json:"fixtures"`
	Results      []measurement `json:"results"`
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "perf-cross:", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	c := &config{
		standaloneDir: "testdata/serenityos",
		embeddedDir:   "testdata/pdf-embedded/serenityos",
		gobig2Bin:     "./bin/gobig2",
		javaBin:       "java",
		tmpDir:        "",
		iters:         5,
		warmup:        1,
		timeout:       30 * time.Second,
		outputFormat:  "pbm",
	}
	var decoderList string
	flag.StringVar(&c.standaloneDir, "standalone-dir", c.standaloneDir, "directory containing standalone .jbig2 fixtures")
	flag.StringVar(&c.embeddedDir, "embedded-dir", c.embeddedDir, "directory containing pdf-embedded .jb2 fixtures + .txt sidecars")
	flag.StringVar(&c.extraCorpusDir, "extra-corpus-dir", "", "additional directory of synthesized fixtures (NAME.jb2 + NAME.txt; optional NAME-embedded.jb2)")
	flag.BoolVar(&c.skipBuiltin, "skip-builtin", false, "skip the in-tree SerenityOS fixture list (use only -extra-corpus-dir)")
	flag.StringVar(&c.gobig2Bin, "gobig2-bin", c.gobig2Bin, "path to the gobig2 CLI binary")
	flag.StringVar(&c.pdfboxJar, "pdfbox-jar", "", "path to pdfbox-app-3.x.y.jar (required when 'pdfbox' is in -decoders)")
	flag.StringVar(&c.javaBin, "java", c.javaBin, "java binary used to invoke PDFBox")
	flag.StringVar(&c.tmpDir, "tmpdir", "", "tempdir for PDF wrappers + decode outputs (default: os.MkdirTemp)")
	flag.IntVar(&c.iters, "iters", c.iters, "iterations per (decoder, fixture); best-of is reported")
	flag.IntVar(&c.warmup, "warmup", c.warmup, "warmup iterations discarded before timing")
	flag.DurationVar(&c.timeout, "timeout", c.timeout, "per-iteration wall-clock cap")
	flag.StringVar(&decoderList, "decoders", "gobig2,jbig2dec,mutool,pdfimages,pdfbox",
		"comma-separated decoder names to include")
	flag.StringVar(&c.outputFormat, "output-format", c.outputFormat,
		"per-decoder output: 'pbm' (decode-only; pdfbox skipped) or 'png' (Go image/png and libpng on decode path; widest compat)")
	flag.StringVar(&c.outMD, "out-md", "-", "markdown output path; '-' for stdout")
	flag.StringVar(&c.outJSON, "out-json", "", "JSON output path; empty to skip")
	flag.Parse()
	switch c.outputFormat {
	case "pbm", "png":
		// ok
	default:
		fmt.Fprintf(os.Stderr, "perf-cross: -output-format must be pbm|png (got %q)\n", c.outputFormat)
		os.Exit(2)
	}
	c.decoders = splitCSV(decoderList)
	return c
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// run is the orchestrator entry point, split from main() for
// testability. Returns nil even when individual decodes fail
// (Error / Skipped on each measurement cell carries that); only
// setup failures (tempdir, gobig2 probe, fixture load) abort.
func run(cfg *config) error {
	if cfg.iters < 1 {
		return fmt.Errorf("--iters must be >= 1 (got %d)", cfg.iters)
	}
	if cfg.warmup < 0 {
		return fmt.Errorf("--warmup must be >= 0 (got %d)", cfg.warmup)
	}

	if cfg.tmpDir == "" {
		d, err := os.MkdirTemp("", "perf-cross-*")
		if err != nil {
			return fmt.Errorf("mkdir tmp: %w", err)
		}
		cfg.tmpDir = d
		defer func() { _ = os.RemoveAll(d) }()
	} else if err := os.MkdirAll(cfg.tmpDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", cfg.tmpDir, err)
	}

	// Required when gobig2 is enabled; otherwise stay "unknown"
	// in the report header.
	cfg.gobig2Version = "unknown"
	if contains(cfg.decoders, "gobig2") {
		v, err := probeGobig2Version(cfg.gobig2Bin)
		if err != nil {
			return fmt.Errorf("probe gobig2 (%s): %w", cfg.gobig2Bin, err)
		}
		cfg.gobig2Version = v
	}

	// Built-in: bundled SerenityOS corpus.
	// Extra: -extra-corpus-dir, populated by
	// scripts/perf/synthesize-corpus.sh on CI.
	var fixtures []fixture
	if !cfg.skipBuiltin {
		bf, err := loadBuiltinFixtures(cfg)
		if err != nil {
			return fmt.Errorf("load builtin fixtures: %w", err)
		}
		fixtures = append(fixtures, bf...)
	}
	if cfg.extraCorpusDir != "" {
		ef, err := loadExtraCorpus(cfg.extraCorpusDir)
		if err != nil {
			return fmt.Errorf("load extra corpus %s: %w", cfg.extraCorpusDir, err)
		}
		fixtures = append(fixtures, ef...)
	}
	if len(fixtures) == 0 {
		return errors.New("no fixtures to bench (check --standalone-dir / --extra-corpus-dir)")
	}

	decoders := buildDecoders(cfg)
	enabled := selectDecoders(decoders, cfg.decoders)

	rpt := &report{
		Header:       "JBIG2 cross-decoder benchmark",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		GobigVersion: cfg.gobig2Version,
		OutputFormat: cfg.outputFormat,
		Iters:        cfg.iters,
		Warmup:       cfg.warmup,
	}
	for _, d := range enabled {
		rpt.Decoders = append(rpt.Decoders, d.Name)
	}
	for _, f := range fixtures {
		rpt.Fixtures = append(rpt.Fixtures, f.Name)
	}

	// Probe each enabled decoder once. Missing binary -> single
	// skip-reason fills every fixture row for that decoder.
	avail := map[string]struct {
		ok     bool
		reason string
	}{}
	for _, d := range enabled {
		ok, reason := d.Available(cfg)
		avail[d.Name] = struct {
			ok     bool
			reason string
		}{ok, reason}
	}

	for _, f := range fixtures {
		// Build the PDF wrapper once per fixture, shared by all
		// kindPDF decoders. On build failure pdfPath stays empty
		// and pdfSkipReason carries the cell text so the inner
		// loop emits exactly one row per (decoder, fixture).
		var pdfPath, pdfSkipReason string
		if anyPDF(enabled) {
			candidate := filepath.Join(cfg.tmpDir, f.Name+".pdf")
			if err := buildPDFForFixture(cfg, f, candidate); err != nil {
				pdfSkipReason = fmt.Sprintf("pdf-wrap: %v", err)
			} else {
				pdfPath = candidate
			}
		}

		for _, d := range enabled {
			a := avail[d.Name]
			if !a.ok {
				rpt.Results = append(rpt.Results, measurement{
					Decoder:    d.Name,
					Fixture:    f.Name,
					Skipped:    true,
					SkipReason: a.reason,
				})
				continue
			}
			var input string
			switch d.Kind {
			case kindStandalone:
				input = f.StandalonePath
			case kindPDF:
				if pdfPath == "" {
					rpt.Results = append(rpt.Results, measurement{
						Decoder:    d.Name,
						Fixture:    f.Name,
						Skipped:    true,
						SkipReason: pdfSkipReason,
					})
					continue
				}
				input = pdfPath
			}
			m := timeDecoder(cfg, d, input, f.Name)
			rpt.Results = append(rpt.Results, m)
		}
	}

	// Sort by (fixture index, decoder index) so the JSON stays
	// stable if a future change reorders the append sites.
	fixIdx := indexOf(rpt.Fixtures)
	decIdx := indexOf(rpt.Decoders)
	sort.SliceStable(rpt.Results, func(i, j int) bool {
		fi, fj := fixIdx[rpt.Results[i].Fixture], fixIdx[rpt.Results[j].Fixture]
		if fi != fj {
			return fi < fj
		}
		return decIdx[rpt.Results[i].Decoder] < decIdx[rpt.Results[j].Decoder]
	})

	if err := writeMarkdown(cfg, rpt); err != nil {
		return fmt.Errorf("emit markdown: %w", err)
	}
	if cfg.outJSON != "" {
		if err := writeJSON(cfg.outJSON, rpt); err != nil {
			return fmt.Errorf("emit json: %w", err)
		}
	}
	return nil
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func indexOf(ss []string) map[string]int {
	m := make(map[string]int, len(ss))
	for i, s := range ss {
		m[s] = i
	}
	return m
}

// probeGobig2Version returns the token after "gobig2" on the
// first line of `<bin> --version`. Failure is fatal so the
// report header reflects an actual binary.
func probeGobig2Version(bin string) (string, error) {
	cmd := exec.Command(bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	fields := strings.Fields(line)
	for i, f := range fields {
		if f == "gobig2" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return strings.TrimSpace(line), nil
}

// loadBuiltinFixtures resolves builtinFixtureSpecs against
// cfg.standaloneDir / cfg.embeddedDir. Missing sidecar is
// non-fatal (PDF wrap path skips the fixture); missing standalone
// file is fatal.
func loadBuiltinFixtures(cfg *config) ([]fixture, error) {
	out := make([]fixture, 0, len(builtinFixtureSpecs))
	for _, spec := range builtinFixtureSpecs {
		f := fixture{Name: spec.name}
		f.StandalonePath = filepath.Join(cfg.standaloneDir, spec.standaloneFile)
		if _, err := os.Stat(f.StandalonePath); err != nil {
			return nil, fmt.Errorf("standalone fixture %s: %w", f.StandalonePath, err)
		}
		embedded := filepath.Join(cfg.embeddedDir, spec.embeddedFile)
		if _, err := os.Stat(embedded); err == nil {
			f.EmbeddedPath = embedded
		}
		sidecar := filepath.Join(cfg.embeddedDir, spec.sidecarFile)
		if data, err := os.ReadFile(sidecar); err == nil {
			w, h, perr := parseDimensions(string(data))
			if perr != nil {
				return nil, fmt.Errorf("parse %s: %w", sidecar, perr)
			}
			f.Width = w
			f.Height = h
		}
		out = append(out, f)
	}
	return out, nil
}

// loadExtraCorpus scans dir for fixture triples:
//
//   - <name>.jb2          standalone (required)
//   - <name>.txt          dimensions sidecar; enables PDF wrap
//   - <name>-embedded.jb2 segment-stream form; PDF decoders skip
//     this fixture when absent
//
// Output is lexicographic by name so the Markdown table stays
// stable across runs.
func loadExtraCorpus(dir string) ([]fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	byName := map[string]*pending{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, "-embedded.jb2"):
			base := strings.TrimSuffix(name, "-embedded.jb2")
			ensure(byName, base).embedded = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".jb2"):
			base := strings.TrimSuffix(name, ".jb2")
			ensure(byName, base).standalone = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".txt"):
			base := strings.TrimSuffix(name, ".txt")
			ensure(byName, base).sidecar = filepath.Join(dir, name)
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]fixture, 0, len(names))
	for _, n := range names {
		p := byName[n]
		if p.standalone == "" {
			// Warn but keep going - a half-populated cache
			// directory shouldn't abort the whole run.
			fmt.Fprintf(os.Stderr, "perf-cross: extra corpus %q has no standalone .jb2; skipping\n", n)
			continue
		}
		f := fixture{Name: n, StandalonePath: p.standalone, EmbeddedPath: p.embedded}
		if p.sidecar != "" {
			data, err := os.ReadFile(p.sidecar)
			if err != nil {
				return nil, fmt.Errorf("read sidecar %s: %w", p.sidecar, err)
			}
			w, h, perr := parseDimensions(string(data))
			if perr != nil {
				return nil, fmt.Errorf("parse %s: %w", p.sidecar, perr)
			}
			f.Width = w
			f.Height = h
		}
		out = append(out, f)
	}
	return out, nil
}

func ensure(m map[string]*pending, k string) *pending {
	if p, ok := m[k]; ok {
		return p
	}
	p := &pending{}
	m[k] = p
	return p
}

// pending accumulates the three files of one fixture during the
// extra-corpus scan.
type pending struct {
	standalone, embedded, sidecar string
}

// parseDimensions extracts `dimensions: WxH` from a sidecar.
// See testdata/pdf-embedded/.../README.txt for the grammar.
func parseDimensions(s string) (int, int, error) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		const key = "dimensions:"
		if !strings.HasPrefix(line, key) {
			continue
		}
		rest := strings.TrimSpace(line[len(key):])
		ix := strings.IndexByte(rest, 'x')
		if ix < 0 {
			return 0, 0, fmt.Errorf("malformed dimensions line: %q", line)
		}
		w, err := strconv.Atoi(strings.TrimSpace(rest[:ix]))
		if err != nil {
			return 0, 0, fmt.Errorf("width: %w", err)
		}
		h, err := strconv.Atoi(strings.TrimSpace(rest[ix+1:]))
		if err != nil {
			return 0, 0, fmt.Errorf("height: %w", err)
		}
		return w, h, nil
	}
	return 0, 0, errors.New("no dimensions line")
}

// buildPDFForFixture wraps f's embedded form into a PDF at
// outPath. Returns an error (not a panic / fatal) when dims or
// embedded form are missing so the caller can mark only PDF rows
// skipped.
func buildPDFForFixture(_ *config, f fixture, outPath string) error {
	if f.Width == 0 || f.Height == 0 {
		return fmt.Errorf("no dimensions for fixture %q", f.Name)
	}
	if f.EmbeddedPath == "" {
		return fmt.Errorf("no embedded form for fixture %q", f.Name)
	}
	jb2, err := os.ReadFile(f.EmbeddedPath)
	if err != nil {
		return fmt.Errorf("read embedded fixture: %w", err)
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return wrapJBIG2InPDF(out, jb2, f.Width, f.Height)
}

// buildDecoders returns the decoder catalog in Markdown-column
// order. selectDecoders filters by the -decoders flag while
// preserving this order.
func buildDecoders(cfg *config) []decoder {
	return []decoder{
		{
			Name: "gobig2",
			Kind: kindStandalone,
			Available: func(c *config) (bool, string) {
				if _, err := os.Stat(c.gobig2Bin); err != nil {
					return false, fmt.Sprintf("not found at %s", c.gobig2Bin)
				}
				return true, ""
			},
			BuildCommand: func(in, out string) (*exec.Cmd, error) {
				dst := filepath.Join(out, "gobig2."+cfg.outputFormat)
				//nolint:gosec // -gobig2-bin is a dev / CI flag; tainting the path is the user's prerogative.
				return exec.Command(cfg.gobig2Bin, "--format="+cfg.outputFormat, in, dst), nil
			},
		},
		{
			Name:        "jbig2dec",
			Kind:        kindStandalone,
			BinaryProbe: "jbig2dec",
			Available:   pathAvailable("jbig2dec"),
			BuildCommand: func(in, out string) (*exec.Cmd, error) {
				dst := filepath.Join(out, "jbig2dec."+cfg.outputFormat)
				//nolint:gosec // -output-format is a dev / CI flag input, intentionally taint-able.
				return exec.Command("jbig2dec", "-t", cfg.outputFormat, "-o", dst, in), nil
			},
		},
		{
			Name:        "mutool",
			Kind:        kindPDF,
			BinaryProbe: "mutool",
			Available:   pathAvailable("mutool"),
			BuildCommand: func(in, out string) (*exec.Cmd, error) {
				// mutool infers format from the output extension
				// (pnm, pbm, png, ... per `mutool draw -h`).
				dst := filepath.Join(out, "mutool."+cfg.outputFormat)
				return exec.Command("mutool", "draw", "-q", "-o", dst, in), nil
			},
		},
		{
			Name:        "pdfimages",
			Kind:        kindPDF,
			BinaryProbe: "pdfimages",
			Available:   pathAvailable("pdfimages"),
			BuildCommand: func(in, out string) (*exec.Cmd, error) {
				prefix := filepath.Join(out, "pdfimg")
				// poppler defaults to PBM/PGM/PPM by source
				// color depth; -png forces 8 bpp grayscale PNG.
				// In pbm mode we want the native PBM path.
				if cfg.outputFormat == "png" {
					return exec.Command("pdfimages", "-png", in, prefix), nil
				}
				return exec.Command("pdfimages", in, prefix), nil
			},
		},
		{
			Name: "pdfbox",
			Kind: kindPDF,
			Available: func(c *config) (bool, string) {
				// pdfbox's `render -format=` is backed by Java
				// ImageIO; png/jpg/gif/bmp/wbmp only. Skip
				// cleanly in pbm mode rather than mixing
				// formats inside one comparison column.
				if c.outputFormat == "pbm" {
					return false, "pdfbox has no PBM ImageIO writer; rerun with -output-format=png"
				}
				if c.pdfboxJar == "" {
					return false, "no -pdfbox-jar provided"
				}
				if _, err := os.Stat(c.pdfboxJar); err != nil {
					return false, fmt.Sprintf("pdfbox-jar not found: %s", c.pdfboxJar)
				}
				if _, err := exec.LookPath(c.javaBin); err != nil {
					return false, fmt.Sprintf("java not on PATH (%s)", c.javaBin)
				}
				return true, ""
			},
			BuildCommand: func(in, out string) (*exec.Cmd, error) {
				prefix := filepath.Join(out, "pdfbox")
				//nolint:gosec // java + pdfbox-jar are dev / CI flag inputs; tainting them is the user's prerogative.
				return exec.Command(cfg.javaBin, "-jar", cfg.pdfboxJar,
					"render", "-format=png", "-i="+in, "-prefix="+prefix), nil
			},
		},
	}
}

// pathAvailable returns an Available func that reports whether
// name is on PATH.
func pathAvailable(name string) func(c *config) (bool, string) {
	return func(c *config) (bool, string) {
		if _, err := exec.LookPath(name); err != nil {
			return false, fmt.Sprintf("%s not on PATH", name)
		}
		return true, ""
	}
}

func selectDecoders(all []decoder, want []string) []decoder {
	if len(want) == 0 {
		return all
	}
	keep := make(map[string]bool, len(want))
	for _, n := range want {
		keep[n] = true
	}
	out := make([]decoder, 0, len(want))
	for _, d := range all {
		if keep[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// timeDecoder runs cfg.warmup discarded invocations followed by
// cfg.iters timed ones, returning a measurement with the best /
// mean / median wall-clock duration in ms. Subprocess failure
// short-circuits and lands in measurement.Error.
func timeDecoder(cfg *config, d decoder, input, fixtureName string) measurement {
	iterOutDir := filepath.Join(cfg.tmpDir, "out-"+d.Name+"-"+fixtureName)
	if err := os.MkdirAll(iterOutDir, 0o750); err != nil {
		return measurement{Decoder: d.Name, Fixture: fixtureName, Error: err.Error()}
	}
	m := measurement{Decoder: d.Name, Fixture: fixtureName, Iters: cfg.iters, OK: true}

	for i := 0; i < cfg.warmup; i++ {
		if err := runOnce(cfg, d, input, iterOutDir); err != nil {
			m.OK = false
			m.Error = fmt.Sprintf("warmup: %v", err)
			return m
		}
	}

	samples := make([]float64, 0, cfg.iters)
	for i := 0; i < cfg.iters; i++ {
		start := time.Now()
		if err := runOnce(cfg, d, input, iterOutDir); err != nil {
			m.OK = false
			m.Error = err.Error()
			return m
		}
		samples = append(samples, float64(time.Since(start))/float64(time.Millisecond))
	}

	sort.Float64s(samples)
	m.BestMs = samples[0]
	m.MedianMs = samples[len(samples)/2]
	var sum float64
	for _, v := range samples {
		sum += v
	}
	m.MeanMs = sum / float64(len(samples))
	return m
}

// runOnce executes one subprocess under cfg.timeout. stdout +
// stderr are captured and surfaced via the returned error so a
// failing decoder is diagnosable without re-running.
func runOnce(cfg *config, d decoder, input, outDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	cmd, err := d.BuildCommand(input, outDir)
	if err != nil {
		return err
	}
	// Rebuild under our context. BuildCommand returns a Cmd with
	// no context; CommandContext is the simplest re-wrap.
	//nolint:gosec // Decoder binaries are dev / CI flag inputs, intentionally taint-able.
	cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		short := strings.TrimSpace(out.String())
		if len(short) > 200 {
			short = short[:200] + "..."
		}
		if short != "" {
			return fmt.Errorf("%w: %s", err, short)
		}
		return err
	}
	return nil
}

// writeMarkdown emits the report as Markdown: a times table
// (rows = fixtures, cols = decoders, values in ms) plus an
// optional speedup-vs-gobig2 table and a notes section for any
// skipped / failed cells.
func writeMarkdown(cfg *config, r *report) error {
	var w io.Writer
	if cfg.outMD == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(cfg.outMD)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	fmt.Fprintf(w, "# %s\n\n", r.Header)
	fmt.Fprintf(w, "- gobig2: %s\n", r.GobigVersion)
	fmt.Fprintf(w, "- Go: %s\n", r.GoVersion)
	fmt.Fprintf(w, "- Platform: %s/%s\n", r.GOOS, r.GOARCH)
	fmt.Fprintf(w, "- Output format: %s\n", r.OutputFormat)
	fmt.Fprintf(w, "- Iterations: %d (warmup %d, best-of reported in ms)\n", r.Iters, r.Warmup)
	fmt.Fprintf(w, "- Generated: %s\n\n", r.GeneratedAt)

	// Index by (fixture, decoder) for table emission.
	cell := make(map[string]measurement)
	for _, m := range r.Results {
		cell[m.Fixture+"|"+m.Decoder] = m
	}

	fmt.Fprintln(w, "## Best-of times (ms)")
	fmt.Fprintln(w)
	// Header row.
	fmt.Fprintf(w, "| fixture |")
	for _, d := range r.Decoders {
		fmt.Fprintf(w, " %s |", d)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| --- |")
	for range r.Decoders {
		fmt.Fprint(w, " ---: |")
	}
	fmt.Fprintln(w)
	for _, fix := range r.Fixtures {
		fmt.Fprintf(w, "| %s |", fix)
		for _, dname := range r.Decoders {
			m := cell[fix+"|"+dname]
			fmt.Fprintf(w, " %s |", formatCell(m))
		}
		fmt.Fprintln(w)
	}

	// Speedup table: other / gobig2, ratio > 1.0 = gobig2 slower.
	// Emitted only when gobig2 + at least one other decoder are
	// in the set.
	others := make([]string, 0, len(r.Decoders))
	for _, d := range r.Decoders {
		if d != "gobig2" {
			others = append(others, d)
		}
	}
	if contains(r.Decoders, "gobig2") && len(others) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "## Speedup vs gobig2 (other / gobig2; >1.0 = gobig2 faster)")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "| fixture |")
		for _, d := range others {
			fmt.Fprintf(w, " %s |", d)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "| --- |")
		for range others {
			fmt.Fprint(w, " ---: |")
		}
		fmt.Fprintln(w)
		for _, fix := range r.Fixtures {
			fmt.Fprintf(w, "| %s |", fix)
			base := cell[fix+"|gobig2"]
			for _, dname := range others {
				other := cell[fix+"|"+dname]
				fmt.Fprintf(w, " %s |", formatSpeedup(base, other))
			}
			fmt.Fprintln(w)
		}
	}

	// One legend line per skipped or failed cell.
	var notes []string
	for _, m := range r.Results {
		if m.Skipped {
			notes = append(notes, fmt.Sprintf("- `%s`/`%s`: skipped (%s)", m.Fixture, m.Decoder, m.SkipReason))
		} else if !m.OK {
			notes = append(notes, fmt.Sprintf("- `%s`/`%s`: error (%s)", m.Fixture, m.Decoder, m.Error))
		}
	}
	if len(notes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "## Notes")
		fmt.Fprintln(w)
		for _, n := range notes {
			fmt.Fprintln(w, n)
		}
	}

	return nil
}

func formatCell(m measurement) string {
	switch {
	case m.Skipped:
		return "skip"
	case !m.OK:
		return "FAIL"
	default:
		return fmt.Sprintf("%.2f", m.BestMs)
	}
}

func formatSpeedup(base, other measurement) string {
	if base.Skipped || !base.OK || other.Skipped || !other.OK {
		return "-"
	}
	if base.BestMs == 0 {
		return "-"
	}
	ratio := other.BestMs / base.BestMs
	return fmt.Sprintf("%.2fx", ratio)
}

func writeJSON(path string, r *report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// anyPDF reports whether the enabled set contains any PDF-input
// decoder. Cheap guard so we don't build a PDF wrapper when no
// consumer needs it.
func anyPDF(ds []decoder) bool {
	for _, d := range ds {
		if d.Kind == kindPDF {
			return true
		}
	}
	return false
}
