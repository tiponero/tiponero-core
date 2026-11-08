TOOLS := tools
BINARY := bin/tiponero

# Resolve the tool binaries from the tools module, then run them from the
# project root. `go -C tools tool air` would run air inside tools/, which makes
# it watch the wrong directory.
TEMPL := $(shell cd $(TOOLS) && go tool -n templ)
AIR := $(shell cd $(TOOLS) && go tool -n air)
GOLANGCI := $(shell cd $(TOOLS) && go tool -n golangci-lint)

# Use a tailwindcss on PATH when there is one (for example inside `nix
# develop`). Otherwise download the pinned standalone binary once.
TAILWIND_VERSION := v3.4.19
TAILWIND ?= $(shell command -v tailwindcss 2>/dev/null)
ifeq ($(TAILWIND),)
TAILWIND := $(CURDIR)/bin/tailwindcss
CSS_DEPS := bin/tailwindcss
endif

.PHONY: generate css build dev lint clean

generate:
	$(TEMPL) generate -path .

css: $(CSS_DEPS)
	$(TAILWIND) -i static/css/input.css -o static/css/output.css --minify

bin/tailwindcss:
	@mkdir -p bin
	@echo "tailwindcss is not on PATH; downloading $(TAILWIND_VERSION)"
	@arch=$$(uname -m); \
	case $$arch in \
		x86_64|amd64) arch=x64 ;; \
		aarch64|arm64) arch=arm64 ;; \
		*) echo "unsupported architecture: $$arch" >&2; exit 1 ;; \
	esac; \
	curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-$$arch" -o $@; \
	chmod +x $@

build: generate css
	go build -o $(BINARY) ./cmd/tiponero

dev:
	$(AIR)

lint:
	$(GOLANGCI) run

clean:
	rm -rf $(BINARY) tmp static/css/output.css
	find . -name "*_templ.go" -delete
