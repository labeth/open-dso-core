// Command dsosessions runs recorded sessions (package session) against a
// scope and compares runs.
//
//	dsosessions run -target native -profile P -demo D -out native.json
//	dsosessions run -target wasm   -profile P -demo D -out wasm.json
//	dsosessions run -target url    -url http://127.0.0.1:8080 -out run.json
//	dsosessions run -target simlab -url http://127.0.0.1:8080 -bench-url http://127.0.0.1:8090 -out hd.json
//	dsosessions run -target bench  -url http://SCOPE:8080 -bench B.json [-minimal-relay] [-sources run|ask|assume] [-dry-run]
//	dsosessions diff A.json B.json [-out DIR]
//	dsosessions pair -profile P -demo D -out DIR     (native and wasm, and their diff)
//	dsosessions capture -url http://SCOPE:8080 -about WHAT -out C.json [-rows id=text,…] [-decode JSON] [-single]
//
// A capture reads the record a stopped scope holds, as it serves it, with
// the rows that set it up, its decoder and what it reports of the record
// (session.CaptureFile): -rows first sets sheet rows (time.tdiv=100us),
// -single then takes one record with SINGLE, and -decode reports the
// packets with that decoder ({"proto":2,"baud":115200,"ch_a":0}) in place
// of the panel's. The demo replays it (session.CaptureFile.Replay,
// simdemo/demotest.Captures) and must report the same.
//
// A bench run restores the scope's state after each session, and with
// -dry-run prints every request and bench action it would make instead.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/labeth/open-dso-core/session"
	"github.com/labeth/open-dso-core/simdemo"
)

// TRLC-LINKS: REQ-DSO-295
func main() {
	if len(os.Args) < 2 {
		usage()
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "dsosessions:", err)
		os.Exit(1)
	}
}

// run runs one command until it ends or the process is interrupted.
// TRLC-LINKS: REQ-DSO-295
func run(cmd string, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch cmd {
	case "run":
		return cmdRun(ctx, args)
	case "diff":
		return cmdDiff(args)
	case "pair":
		return cmdPair(ctx, args)
	case "capture":
		return cmdCapture(ctx, args)
	}
	usage()
	return nil
}

// TRLC-LINKS: REQ-DSO-295
func usage() {
	fmt.Fprintln(os.Stderr, "usage: dsosessions run|diff|pair|capture [flags] (dsosessions run -h)")
	os.Exit(2)
}

// runFlags are the run command's flags.
type runFlags struct {
	target, url, benchURL, bench, sources, profile, demo, sessions, only, out, md, tmp string
	minimalRelay, dryRun, restore                                                      bool
	settle                                                                             time.Duration
}

// TRLC-LINKS: REQ-DSO-295
func (f *runFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.target, "target", "native", "native, wasm, url, simlab or bench")
	fs.StringVar(&f.url, "url", "", "the scope's web API (url, simlab, bench)")
	fs.StringVar(&f.benchURL, "bench-url", "http://127.0.0.1:8090", "the simulator's bench page (simlab)")
	fs.StringVar(&f.bench, "bench", "", "the bench file: wiring and sources (bench)")
	fs.StringVar(&f.sources, "sources", session.SourcesAsk, "bench sources: run their commands, ask the operator, or assume them selected")
	fs.StringVar(&f.profile, "profile", "", "the scope's profile.yaml (native, wasm)")
	fs.StringVar(&f.demo, "demo", "", "the scope's demo.json (native, wasm)")
	fs.StringVar(&f.sessions, "sessions", filepath.Join(session.Dir(), "sessions"), "the sessions directory")
	fs.StringVar(&f.only, "only", "", "comma-separated session names to run (default: all)")
	fs.StringVar(&f.out, "out", "", "write the run as JSON here")
	fs.StringVar(&f.md, "md", "", "write the run's summary as Markdown here")
	fs.StringVar(&f.tmp, "browser-tmp", defaultTmp(), "the browser's temporary directory, on a real disk (wasm)")
	fs.BoolVar(&f.minimalRelay, "minimal-relay", false, "skip every session and start or restore that clicks front-end relays")
	fs.BoolVar(&f.dryRun, "dry-run", false, "print every request and bench action instead of making it (bench, url)")
	fs.BoolVar(&f.restore, "restore", false, "restore the scope's state after each session (always on for bench)")
	fs.DurationVar(&f.settle, "settle", 20*time.Second, "the longest wait for the panel to settle after a step")
}

// defaultTmp is a temporary directory on the home disk.
// TRLC-LINKS: REQ-DSO-295
func defaultTmp() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "dsosessions", "tmp")
}

// TRLC-LINKS: REQ-DSO-295
func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var f runFlags
	f.register(fs)
	_ = fs.Parse(args) // ExitOnError
	sessions, base, err := loadSessions(f.sessions, f.only)
	if err != nil {
		return err
	}
	r, err := runTarget(ctx, f, sessions, base)
	if err != nil {
		return err
	}
	return writeRun(r, f.out, f.md)
}

// loadSessions loads the sessions directory and keeps the named ones.
// TRLC-LINKS: REQ-DSO-295
func loadSessions(dir, only string) ([]*session.Session, session.Baseline, error) {
	all, base, err := session.Load(dir)
	if err != nil {
		return nil, base, err
	}
	if only == "" {
		return all, base, nil
	}
	names := strings.Split(only, ",")
	var out []*session.Session
	for _, s := range all {
		if slices.Contains(names, s.Name) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, base, fmt.Errorf("no session named %s", only)
	}
	return out, base, nil
}

// runTarget starts the target f names and runs the sessions on it.
// TRLC-LINKS: REQ-DSO-295
func runTarget(ctx context.Context, f runFlags, sessions []*session.Session, base session.Baseline) (*session.Run, error) {
	t := &session.Target{Name: f.target, Log: os.Stderr, MinimalRelay: f.minimalRelay, Restore: f.restore, SettleMax: f.settle}
	switch f.target {
	case "native":
		url, stop, err := startNative(ctx, f.profile, f.demo)
		if err != nil {
			return nil, err
		}
		defer stop()
		t.Client = session.NewClient(url)
		sb := session.SimBench{C: t.Client}
		t.Signals, t.Input = sb, sb
	case "wasm":
		if err := os.MkdirAll(f.tmp, 0o750); err != nil {
			return nil, err
		}
		site, err := os.MkdirTemp(f.tmp, "site")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(site) }()
		if err := buildSite(ctx, f.profile, f.demo, site); err != nil {
			return nil, err
		}
		url, stop, err := session.StartWasm(ctx, site, f.tmp, os.Stderr)
		if err != nil {
			return nil, err
		}
		defer stop()
		t.Client = session.NewClient(url)
		sb := session.SimBench{C: t.Client}
		t.Signals, t.Input = sb, sb
	case "url":
		if f.url == "" {
			return nil, errors.New("-url is required")
		}
		t.Client = session.NewClient(f.url)
		sb := session.SimBench{C: t.Client}
		t.Signals = sb
		if !f.dryRun {
			t.Input = sb
		}
	case "simlab":
		t.Client = session.NewClient(f.url)
		t.Signals = session.SimLab{C: session.NewClient(f.benchURL)}
	case "bench":
		if f.url == "" || f.bench == "" {
			return nil, errors.New("-url and -bench are required")
		}
		data, err := os.ReadFile(f.bench)
		if err != nil {
			return nil, err
		}
		b, err := session.ParseBench(data)
		if err != nil {
			return nil, err
		}
		t.Client, t.Restore = session.NewClient(f.url), true
		d := &session.BenchDriver{Bench: b, Mode: f.sources, Out: os.Stderr}
		t.Signals = d
		if f.dryRun {
			d.Plan = os.Stdout
		}
		printBench(os.Stdout, b, f, sessions)
	default:
		return nil, fmt.Errorf("target %q: want native, wasm, url, simlab or bench", f.target)
	}
	if f.dryRun {
		t.Client.Plan, t.Log = os.Stdout, os.Stdout
	}
	return t.RunAll(ctx, sessions, base), nil
}

// printBench prints what a bench run needs before it starts: the wiring,
// the bench's notes and which sessions it will run, skip or click relays in.
// TRLC-LINKS: REQ-DSO-295
func printBench(w io.Writer, b *session.Bench, f runFlags, sessions []*session.Session) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }
	p("Bench %s at %s (sources: %s, minimal-relay %v, restore after each session)", b.Name, f.url, f.sources, f.minimalRelay)
	p("Wiring:")
	for _, l := range b.Wiring {
		p("  - %s", l)
	}
	for _, l := range b.Notes {
		p("  note: %s", l)
	}
	d := &session.BenchDriver{Bench: b}
	p("Sessions:")
	for _, s := range sessions {
		state := "runs"
		switch ok, why := d.Can(s.Signals); {
		case !ok:
			state = "skipped: " + why
		case f.minimalRelay && s.Relays:
			state = "skipped: clicks relays"
		case slices.Contains(s.Needs, session.NeedBoardInput):
			state = "skipped: needs the board's panel input"
		case s.Relays:
			state = "runs, CLICKS RELAYS"
		}
		src := ""
		if s2, ok := b.Source(s.Signals); ok && len(s.Signals) > 0 {
			src = " [source " + s2.ID + "]"
		}
		p("  %-28s %s%s", s.Name, state, src)
	}
}

// startNative runs the native demo of a profile and demo file in this
// process, as cmd/dsodemo composes it, and returns its base URL.
// TRLC-LINKS: REQ-DSO-295
func startNative(ctx context.Context, profilePath, demoPath string) (string, func(), error) {
	profileYAML, demoJSON, err := readDemo(profilePath, demoPath)
	if err != nil {
		return "", nil, err
	}
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		return "", nil, err
	}
	dctx, cancel := context.WithCancel(ctx)
	var web http.Handler
	o := simdemo.Options{Logf: func(string, ...any) {}, Version: "demo", Serve: func(h http.Handler) { web = h }}
	if _, _, err := simdemo.Start(dctx, profileYAML, d, o); err != nil {
		cancel()
		return "", nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(dctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return "", nil, err
	}
	srv := &http.Server{Handler: web, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() { _ = srv.Close(); cancel() }, nil
}

// buildSite builds the browser demo's static site into dir.
// TRLC-LINKS: REQ-DSO-295
func buildSite(ctx context.Context, profilePath, demoPath, dir string) error {
	profileYAML, demoJSON, err := readDemo(profilePath, demoPath)
	if err != nil {
		return err
	}
	return simdemo.BuildSite(ctx, profileYAML, demoJSON, dir)
}

// TRLC-LINKS: REQ-DSO-295
func readDemo(profilePath, demoPath string) ([]byte, []byte, error) {
	if profilePath == "" || demoPath == "" {
		return nil, nil, errors.New("-profile and -demo are required")
	}
	p, err := os.ReadFile(profilePath) //nolint:gosec // G304: the user names the scope's files
	if err != nil {
		return nil, nil, err
	}
	d, err := os.ReadFile(demoPath) //nolint:gosec // G304: the user names the scope's files
	return p, d, err
}

// writeRun writes the run's JSON and Markdown where asked, and the
// Markdown to stdout.
// TRLC-LINKS: REQ-DSO-295
func writeRun(r *session.Run, out, md string) error {
	if out != "" {
		b, err := json.MarshalIndent(r, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil { //nolint:gosec // G306: a report, not a secret
			return err
		}
	}
	w := io.Writer(os.Stdout)
	if md != "" {
		fh, err := os.Create(md) //nolint:gosec // G304: the user names the report
		if err != nil {
			return err
		}
		defer func() { _ = fh.Close() }()
		w = io.MultiWriter(os.Stdout, fh)
	}
	return r.WriteMarkdown(w)
}

// TRLC-LINKS: REQ-DSO-295
func readRun(path string) (*session.Run, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the user names the run
	if err != nil {
		return nil, err
	}
	var r session.Run
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// TRLC-LINKS: REQ-DSO-295
func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	out := fs.String("out", "", "write diff.json and diff.md into this directory")
	_ = fs.Parse(args) // ExitOnError
	if fs.NArg() != 2 {
		return errors.New("diff takes two run files")
	}
	a, err := readRun(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := readRun(fs.Arg(1))
	if err != nil {
		return err
	}
	return writeDiff(session.Compare(a, b), *out)
}

// writeDiff prints the diff's table and writes it as JSON and Markdown
// into dir, when one is given.
// TRLC-LINKS: REQ-DSO-295
func writeDiff(d session.Diff, dir string) error {
	w := io.Writer(os.Stdout)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		b, err := json.MarshalIndent(d, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "diff.json"), b, 0o644); err != nil { //nolint:gosec // G306: a report
			return err
		}
		fh, err := os.Create(filepath.Join(dir, "diff.md")) //nolint:gosec // G304: the user names the directory
		if err != nil {
			return err
		}
		defer func() { _ = fh.Close() }()
		w = io.MultiWriter(os.Stdout, fh)
	}
	return d.WriteMarkdown(w)
}

// TRLC-LINKS: REQ-DSO-295
func cmdPair(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	var f runFlags
	f.register(fs)
	failOnDiff := fs.Bool("fail-on-diff", false, "exit 1 when a field differs")
	_ = fs.Parse(args) // ExitOnError
	if f.out == "" {
		return errors.New("-out DIR is required")
	}
	if err := os.MkdirAll(f.out, 0o750); err != nil {
		return err
	}
	sessions, base, err := loadSessions(f.sessions, f.only)
	if err != nil {
		return err
	}
	runs := map[string]*session.Run{}
	for _, target := range []string{"native", "wasm"} {
		g := f
		g.target = target
		log.Printf("pair: %s", target)
		r, err := runTarget(ctx, g, sessions, base)
		if err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		runs[target] = r
		if err := writeRun(r, filepath.Join(f.out, target+".json"), filepath.Join(f.out, target+".md")); err != nil {
			return err
		}
	}
	d := session.Compare(runs["native"], runs["wasm"])
	if err := writeDiff(d, f.out); err != nil {
		return err
	}
	if *failOnDiff && d.Count().Diff > 0 {
		return fmt.Errorf("%d fields differ between the native and the WebAssembly demo", d.Count().Diff)
	}
	return nil
}

// cmdCapture records the record a scope holds (session.RecordCapture).
// TRLC-LINKS: REQ-DSO-295
func cmdCapture(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("capture", flag.ExitOnError)
	url := fs.String("url", "", "the scope's web API")
	about := fs.String("about", "", "what is on the inputs")
	out := fs.String("out", "", "write the capture here")
	single := fs.Bool("single", false, "take one record with SINGLE first, and wait until the scope holds it")
	rows := fs.String("rows", "", "comma-separated sheet rows to set first, id=text (time.tdiv=100us,ch1.vdiv=1V)")
	decode := fs.String("decode", "", `the decoder the packets are reported with, as JSON ({"proto":2,"baud":115200,"ch_a":0}); default: the panel's`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || *out == "" {
		return errors.New("capture needs -url and -out")
	}
	c := session.NewClient(*url)
	for kv := range strings.SplitSeq(*rows, ",") {
		if kv == "" {
			continue
		}
		id, text, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("-rows: %q is not id=text", kv)
		}
		if err := session.SetRow(ctx, c, id, text); err != nil {
			return err
		}
	}
	if *single {
		if err := c.Post(ctx, "/api/panel", map[string]any{"button": "single"}); err != nil {
			return err
		}
		if err := waitStopped(ctx, c, 30*time.Second); err != nil {
			return err
		}
	}
	f, err := session.RecordCapture(ctx, c, *about)
	if err != nil {
		return err
	}
	if *decode != "" {
		var d session.CaptureDecode
		if err := json.Unmarshal([]byte(*decode), &d); err != nil {
			return fmt.Errorf("-decode: %w", err)
		}
		if err := f.WithDecode(&d); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil { //nolint:gosec // G306: a capture is the operator's file
		return err
	}
	log.Printf("captured %s %s: %d reported fields, %d rows → %s", f.Scope.Vendor, f.Scope.Model, len(f.Reported), len(f.Rows), *out)
	return nil
}

// waitStopped waits until the scope at c has stopped running.
// TRLC-LINKS: REQ-DSO-295
func waitStopped(ctx context.Context, c *session.Client, limit time.Duration) error {
	end := time.Now().Add(limit)
	for time.Now().Before(end) {
		var st struct {
			Running *bool `json:"running"`
		}
		if err := c.JSON(ctx, http.MethodGet, "/api/status", nil, &st); err != nil {
			return err
		}
		if st.Running != nil && !*st.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("the scope did not take its single record (is its trigger met?)")
}
