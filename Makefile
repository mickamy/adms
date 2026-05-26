APP_NAME       = adms
BUILD_DIR      = bin
DOCKER_COMPOSE ?= docker compose

# Standalone tailwindcss CLI; downloaded into bin/ on first use of
# `make ui-css` and reused thereafter. We pin a v3 release because the
# template class names target v3 syntax.
TAILWIND_VERSION = 3.4.17
TAILWIND_OS_RAW  := $(shell uname -s)
TAILWIND_ARCH_RAW := $(shell uname -m)
ifeq ($(TAILWIND_OS_RAW),Darwin)
  TAILWIND_OS = macos
else
  TAILWIND_OS = $(shell echo $(TAILWIND_OS_RAW) | tr '[:upper:]' '[:lower:]')
endif
ifeq ($(TAILWIND_ARCH_RAW),x86_64)
  TAILWIND_ARCH = x64
else ifeq ($(TAILWIND_ARCH_RAW),aarch64)
  TAILWIND_ARCH = arm64
else ifeq ($(TAILWIND_ARCH_RAW),arm64)
  TAILWIND_ARCH = arm64
else
  $(error unsupported architecture for tailwindcss: $(TAILWIND_ARCH_RAW); see https://github.com/tailwindlabs/tailwindcss/releases for available targets)
endif
TAILWIND_BIN = $(BUILD_DIR)/tailwindcss
TAILWIND_URL = https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)

.PHONY: all build install uninstall clean test test-integration compose-up compose-down lint ui-css

all: build

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) .

install:
	@echo "Installing $(APP_NAME)..."
	@bin_dir=$$(go env GOBIN); \
	if [ -z "$$bin_dir" ]; then \
		bin_dir=$$(go env GOPATH)/bin; \
	fi; \
	mkdir -p "$$bin_dir"; \
	echo "Installing to $$bin_dir/$(APP_NAME)"; \
	go build -o "$$bin_dir/$(APP_NAME)" .

uninstall:
	@echo "Uninstalling $(APP_NAME)..."
	@bin_dir=$$(go env GOBIN); \
	if [ -z "$$bin_dir" ]; then \
		bin_dir=$$(go env GOPATH)/bin; \
	fi; \
	echo "Removing $$bin_dir/$(APP_NAME)"; \
	rm -f "$$bin_dir/$(APP_NAME)"

clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)

test:
	go test ./... -race

test-integration:
	go test -tags=integration ./... -race $(GOTESTFLAGS)

compose-up:
	$(DOCKER_COMPOSE) up -d --wait

compose-down:
	$(DOCKER_COMPOSE) down

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed"; \
		exit 1; \
	}
	golangci-lint run

$(TAILWIND_BIN):
	@mkdir -p $(BUILD_DIR)
	@echo "Downloading tailwindcss $(TAILWIND_VERSION) for $(TAILWIND_OS)-$(TAILWIND_ARCH)..."
	@curl -fsSL -o $@ $(TAILWIND_URL)
	@chmod +x $@

UI_CSS_OUT  = internal/ui/static/css/tailwind.css
UI_CSS_DEPS = tailwind.config.js \
              internal/ui/static/css/input.css \
              $(wildcard internal/ui/templates/*.html)

# File-based target so re-invocations are no-ops when nothing the bundle
# depends on has changed; the `ui-css` phony alias above just gives a
# friendlier command name. The trailing `touch` updates the mtime even
# when the CLI emits byte-identical output (which happens when no class
# names have actually changed), so subsequent `make ui-css` invocations
# can short-circuit instead of looping on a stale-looking timestamp.
$(UI_CSS_OUT): $(TAILWIND_BIN) $(UI_CSS_DEPS)
	@echo "Rebuilding $@..."
	$(TAILWIND_BIN) -c tailwind.config.js \
		-i internal/ui/static/css/input.css \
		-o $@ \
		--minify
	@touch $@

ui-css: $(UI_CSS_OUT)
