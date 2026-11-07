//go:build js && wasm

// Command dsodemo-wasm is the browser demo's engine: the demo (package
// simdemo) as WebAssembly, run by the page's worker (engine.js). The worker
// sets globalThis.dsoDemoConfig before it runs the module:
//
//	{profile: "<profile.yaml>", demo: "<demo.json>", stored: {store: {key: base64}}}
//
// and globalThis.dsoHost, which hears the board:
//
//	present(rgba Uint8ClampedArray, w, h)  a picture the display presented
//	lamps(names)                           the lit lamps
//	persist(store, key, base64)            a store's change, to keep
//	log(line)
//
// Once the demo runs it sets globalThis.dsoServe(request) → Promise of a
// response, which the service worker's requests reach through the worker:
// request {method, url, headers: [[name, value]], body: Uint8Array|null},
// response {status, headers: [[name, value]], body: Uint8Array}.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/labeth/open-dso-core/simdemo"
)

// presentEvery is the least time between two pictures handed to the page:
// the page draws at most this often, and the copy to JS costs the one
// thread the demo has.
const presentEvery = time.Second / 30

func main() {
	cfg := js.Global().Get("dsoDemoConfig")
	host := js.Global().Get("dsoHost")
	logf := func(format string, a ...any) { host.Call("log", fmt.Sprintf(format, a...)) }
	if cfg.IsUndefined() || host.IsUndefined() {
		panic("dsodemo-wasm: no dsoDemoConfig or dsoHost")
	}
	d, err := simdemo.ParseDemo([]byte(cfg.Get("demo").String()))
	if err != nil {
		logf("%v", err)
		return
	}
	var lcd struct {
		sync.Mutex
		at  time.Time
		arr js.Value
	}
	o := simdemo.Options{
		Logf: logf, Version: "demo", Stored: stored(cfg.Get("stored")),
		Persist: func(store, key string, val []byte) {
			host.Call("persist", store, key, base64.StdEncoding.EncodeToString(val))
		},
		OnLamps: func(on []string) {
			list := make([]any, len(on))
			for i, l := range on {
				list[i] = l
			}
			host.Call("lamps", js.ValueOf(list))
		},
		OnPresent: func(rgba []byte, w, h int) {
			lcd.Lock()
			defer lcd.Unlock()
			if time.Since(lcd.at) < presentEvery {
				return
			}
			lcd.at = time.Now()
			arr := js.Global().Get("Uint8ClampedArray").New(len(rgba))
			js.CopyBytesToJS(arr, rgba)
			host.Call("present", arr, w, h)
		},
	}
	var handler http.Handler
	o.Serve = func(h http.Handler) { handler = h }
	if _, _, err := simdemo.Start(context.Background(), []byte(cfg.Get("profile").String()), d, o); err != nil {
		logf("demo: %v", err)
		host.Call("failed", err.Error())
		return
	}
	js.Global().Set("dsoServe", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return serve(handler, args[0])
	}))
	host.Call("ready")
	select {}
}

// stored is the stores' content the page kept: store to key to value.
func stored(v js.Value) map[string]map[string][]byte {
	out := map[string]map[string][]byte{}
	if v.IsUndefined() || v.IsNull() {
		return out
	}
	stores := js.Global().Get("Object").Call("keys", v)
	for i := range stores.Length() {
		name := stores.Index(i).String()
		kv := v.Get(name)
		keys := js.Global().Get("Object").Call("keys", kv)
		m := map[string][]byte{}
		for j := range keys.Length() {
			k := keys.Index(j).String()
			if b, err := base64.StdEncoding.DecodeString(kv.Get(k).String()); err == nil {
				m[k] = b
			}
		}
		out[name] = m
	}
	return out
}

// serve answers one request through h on its own goroutine and returns the
// promise of the response.
func serve(h http.Handler, req js.Value) any {
	method := req.Get("method").String()
	url := req.Get("url").String()
	var body []byte
	if b := req.Get("body"); !b.IsUndefined() && !b.IsNull() {
		body = make([]byte, b.Get("length").Int())
		js.CopyBytesToGo(body, b)
	}
	hdr := http.Header{}
	if hs := req.Get("headers"); !hs.IsUndefined() {
		for i := range hs.Length() {
			hdr.Add(hs.Index(i).Index(0).String(), hs.Index(i).Index(1).String())
		}
	}
	var exec js.Func
	exec = js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve := args[0]
		go func() {
			defer exec.Release()
			r, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
			if err != nil {
				resolve.Invoke(reply(http.StatusBadRequest, http.Header{}, []byte(err.Error())))
				return
			}
			r.Header = hdr
			r.RemoteAddr = "127.0.0.1:1"
			w := &recorder{h: http.Header{}, status: http.StatusOK}
			h.ServeHTTP(w, r)
			resolve.Invoke(reply(w.status, w.h, w.body.Bytes()))
		}()
		return nil
	})
	return js.Global().Get("Promise").New(exec)
}

// reply is a response as the worker hands it on.
func reply(status int, h http.Header, body []byte) js.Value {
	hs := make([]any, 0, len(h))
	for k, vs := range h {
		for _, v := range vs {
			hs = append(hs, []any{k, v})
		}
	}
	arr := js.Global().Get("Uint8Array").New(len(body))
	js.CopyBytesToJS(arr, body)
	return js.ValueOf(map[string]any{"status": status, "headers": hs, "body": arr})
}

// recorder is a response held whole until the handler returns.
type recorder struct {
	h      http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.h }

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
}

func (r *recorder) Write(p []byte) (int, error) {
	r.wrote = true
	return r.body.Write(p)
}

// ReadFrom keeps io.Copy into the response a plain copy.
func (r *recorder) ReadFrom(src io.Reader) (int64, error) {
	r.wrote = true
	return r.body.ReadFrom(src)
}
