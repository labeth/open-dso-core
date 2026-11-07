# The browser demo's test matrix against each scope's WebAssembly build in
# headless Chromium (simdemo/matrix_test.go): the scopes' checkouts beside
# this one, or CML= and HD= naming them.
CML ?= ../open-sds1000cml
HD ?= ../open-sds800xhd
GO ?= go

.PHONY: demo-test
demo-test:
	DEMO_PROFILE=$(abspath $(CML)/app/profile.yaml) DEMO_FILE=$(abspath $(CML)/demo/demo.json) \
		$(GO) test -count=1 -run TestDemoMatrix -timeout 60m -v ./simdemo
	DEMO_PROFILE=$(abspath $(HD)/demo/profile.yaml) DEMO_FILE=$(abspath $(HD)/demo/demo.json) \
		$(GO) test -count=1 -run TestDemoMatrix -timeout 60m -v ./simdemo
