package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Dir is this package's source directory: its sessions/ and the browser
// bridge live there.
// TRLC-LINKS: REQ-DSO-295
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// ErrNoBrowser is a browser target on a host without Playwright.
var ErrNoBrowser = errors.New("session: playwright is not installed")

// StartWasm serves a built demo site on a local port, opens it in headless
// Chromium through wasmbridge.mjs and returns the bridge's base URL, where
// the browser demo's web API answers. tmp is the browser's temporary
// directory (a real disk: a tmpfs one fills); stop ends both.
// TRLC-LINKS: REQ-DSO-295
func StartWasm(ctx context.Context, siteDir, tmp string, log io.Writer) (base string, stop func(), err error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: http.FileServer(http.Dir(siteDir)), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	site := "http://" + ln.Addr().String() + "/"
	if tmp != "" {
		if err := os.MkdirAll(tmp, 0o750); err != nil {
			_ = srv.Close()
			return "", nil, err
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, "node", filepath.Join(Dir(), "wasmbridge.mjs"), site) //nolint:gosec // G204: the package's own bridge script
	cmd.Env = os.Environ()
	if tmp != "" {
		cmd.Env = append(cmd.Env, "TMPDIR="+tmp)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = srv.Close()
		return "", nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = srv.Close()
		return "", nil, err
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		cancel()
		_ = srv.Close()
		return "", nil, err
	}
	stop = func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cancel()
			<-done
		}
		cancel()
		_ = srv.Close()
	}
	sc := bufio.NewScanner(out)
	ready := make(chan string, 1)
	go func() {
		for sc.Scan() {
			l := sc.Text()
			if strings.HasPrefix(l, "READY ") || strings.HasPrefix(l, "SKIP") {
				ready <- l
			} else if log != nil {
				_, _ = fmt.Fprintln(log, l)
			}
		}
		close(ready)
	}()
	select {
	case l, ok := <-ready:
		switch {
		case !ok:
			stop()
			return "", nil, errors.New("session: the browser bridge ended before it was ready")
		case strings.HasPrefix(l, "SKIP"):
			stop()
			return "", nil, ErrNoBrowser
		}
		return strings.TrimPrefix(l, "READY "), stop, nil
	case <-time.After(3 * time.Minute):
		stop()
		return "", nil, errors.New("session: the browser demo did not start in 3 minutes")
	}
}
