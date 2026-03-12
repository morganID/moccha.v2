.PHONY: all build build-darwin-arm64 build-linux-amd64 clean run run-debug run-prod run-cloudflare run-cloudflare-debug embed-cloudflare

all: build-darwin-arm64 build-linux-amd64

# Build for current platform
build:
	go build -o moccha ./cmd/server

# Build for Mac M1 (Apple Silicon)
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o moccha-darwin-arm64 ./cmd/server
	tar -czvf moccha-darwin-arm64.tar.gz moccha-darwin-arm64
	rm moccha-darwin-arm64

# Build for Linux (x86_64)
build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o moccha-linux-amd64 ./cmd/server
	tar -czvf moccha-linux-amd64.tar.gz moccha-linux-amd64
	rm moccha-linux-amd64

# Detect OS and Architecture
OS := $(shell uname -s)
ARCH := $(shell uname -m)

# Download URL for cloudflared (Cloudflare Tunnel)
ifeq ($(OS),Darwin)
	ifeq ($(ARCH),arm64)
		CLOUDFLARED_URL = https://github.com/cloudflare/cloudflared/releases/download/2026.3.0/cloudflared-darwin-arm64.tgz
	endif
	ifeq ($(ARCH),x86_64)
		CLOUDFLARED_URL = https://github.com/cloudflare/cloudflared/releases/download/2026.3.0/cloudflared-darwin-amd64.tgz
	endif
endif
ifeq ($(OS),Linux)
	ifeq ($(ARCH),x86_64)
		CLOUDFLARED_URL = https://github.com/cloudflare/cloudflared/releases/download/2026.3.0/cloudflared-linux-amd64.tgz
	endif
	ifeq ($(ARCH),aarch64)
		CLOUDFLARED_URL = https://github.com/cloudflare/cloudflared/releases/download/2026.3.0/cloudflared-linux-arm64.tgz
	endif
endif

embed-cloudflare:
	@echo "Downloading cloudflared for $(OS)/$(ARCH)..."
	@mkdir -p cmd/server/cloudflared
	@if [ -f "cmd/server/cloudflared/cloudflared" ]; then \
		echo "cloudflared already exists, skipping download"; \
	elif [ -z "$(CLOUDFLARED_URL)" ]; then \
		echo "Unsupported OS/Architecture: $(OS)/$(ARCH)"; exit 1; \
	else \
		curl -fsSL $(CLOUDFLARED_URL) | tar xz -C cmd/server/cloudflared; \
		chmod +x cmd/server/cloudflared/cloudflared; \
	fi
	go build -o moccha ./cmd/server

clean:
	rm -f moccha
	rm -f cmd/server/cloudflared/cloudflared

run:
	./moccha -port 8080 -token mysecret

run-debug:
	./moccha -debug -port 8080 -token mysecret

run-prod:
	./moccha -port 8080 -token mysecret

run-cloudflare:
	./moccha -cloudflare -port 8080 -token mysecret

run-cloudflare-debug:
	./moccha -cloudflare -debug -port 8080 -token mysecret
