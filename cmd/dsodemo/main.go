// Command dsodemo is the demo as a plain local web server, for debugging
// the demo on any OS: the same composition as the browser's WebAssembly
// (package simdemo), with the scope's web UI at / and the bench page (front
// panel, screen, signal generator) at /demo/.
//
//	go run ./cmd/dsodemo -profile ../open-sds1000cml/app/profile.yaml -demo ../open-sds1000cml/demo/demo.json
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labeth/open-dso-core/simdemo"
)

func main() {
	profilePath := flag.String("profile", "", "the scope's profile.yaml")
	demoPath := flag.String("demo", "", "the scope's demo.json")
	listen := flag.String("listen", "127.0.0.1:8090", "the address to serve on")
	quiet := flag.Bool("quiet", false, "log only failures")
	flag.Parse()
	if err := run(*profilePath, *demoPath, *listen, *quiet); err != nil {
		fmt.Fprintln(os.Stderr, "dsodemo:", err)
		os.Exit(1)
	}
}

func run(profilePath, demoPath, listen string, quiet bool) error {
	if profilePath == "" || demoPath == "" {
		return errors.New("-profile and -demo are required")
	}
	profileYAML, err := os.ReadFile(profilePath) //nolint:gosec // G304: the user names the scope's files
	if err != nil {
		return err
	}
	demoJSON, err := os.ReadFile(demoPath) //nolint:gosec // G304: the user names the scope's files
	if err != nil {
		return err
	}
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logf := log.Printf
	if quiet {
		logf = func(string, ...any) {}
	}
	var web http.Handler
	o := simdemo.Options{Logf: logf, Version: "demo", Serve: func(h http.Handler) { web = h }}
	if _, _, err := simdemo.Start(ctx, profileYAML, d, o); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/demo/", simdemo.SiteHandler("/demo/", simdemo.Mode{Mode: "native", UI: "/"}, profileYAML, demoJSON))
	mux.Handle("/", web)
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close() // the demo is ending: parked long polls are cut off
	}()
	log.Printf("demo of %s: web UI http://%s/ · bench http://%s/demo/", d.Name, listen, listen)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
