GOIMPORTS := $(shell command -v goimports 2>/dev/null || { gopath=$$(go env GOPATH 2>/dev/null); if [ -n "$$gopath" ]; then echo "$$gopath/bin/goimports"; fi; })
LOCAL_PKG := github.com/sonicore/server

.PHONY: fmt fmt-check

fmt:
	@if [ -z "$(GOIMPORTS)" ]; then echo "goimports not found: go install golang.org/x/tools/cmd/goimports@latest" >&2; exit 1; fi
	$(GOIMPORTS) -w -local $(LOCAL_PKG) .

fmt-check:
	@if [ -z "$(GOIMPORTS)" ]; then echo "goimports not found: go install golang.org/x/tools/cmd/goimports@latest" >&2; exit 1; fi
	@diff=$$($(GOIMPORTS) -l -local $(LOCAL_PKG) .); \
	if [ -n "$$diff" ]; then \
		echo "Files need formatting (run 'make fmt'):"; \
		echo "$$diff"; \
		exit 1; \
	fi
