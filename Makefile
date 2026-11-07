# Conveniences over the core module.
#
#   make sessions - the recorded sessions (session/sessions) against TARGET:
#                   pair (the default: the native and the WebAssembly demo of
#                   PROFILE and DEMO, and their diff), native, wasm, url (a
#                   running scope or demo at URL), simlab (a simulator at URL
#                   with its bench page at BENCH_URL) or bench (a real scope
#                   at URL driven as BENCH says; dry-run it with
#                   ARGS=-dry-run). PROFILE and DEMO default to the core's
#                   test scope; ARGS adds flags (-only a,b, -minimal-relay,
#                   -sources run|ask|assume). Output in build/sessions/

GO ?= go
TARGET ?= pair
PROFILE ?= simdemo/testdata/profile.yaml
DEMO ?= simdemo/testdata/demo.json
URL ?=
BENCH ?=
BENCH_URL ?= http://127.0.0.1:8090
SESSIONS_OUT ?= build/sessions

.PHONY: sessions

sessions:
	mkdir -p $(SESSIONS_OUT)
ifeq ($(TARGET),pair)
	$(GO) run ./cmd/dsosessions pair -profile $(PROFILE) -demo $(DEMO) -out $(SESSIONS_OUT) $(ARGS)
else
	$(GO) run ./cmd/dsosessions run -target $(TARGET) -profile $(PROFILE) -demo $(DEMO) -url "$(URL)" \
		-bench "$(BENCH)" -bench-url $(BENCH_URL) -out $(SESSIONS_OUT)/$(TARGET).json -md $(SESSIONS_OUT)/$(TARGET).md $(ARGS)
endif
