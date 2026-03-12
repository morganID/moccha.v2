.PHONY: all build build-darwin-arm64 build-linux-amd64 clean run

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

embed-ngrok:
	@echo "Downloading ngrok..."
	@if [ -f "cmd/server/ngrok" ]; then \
		echo "ngrok already exists, skipping download"; \
	else \
		curl -fsSL https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-amd64.tgz | tar xz -C cmd/server; \
		chmod +x cmd/server/ngrok; \
	fi
	go build -o moccha ./cmd/server

clean:
	rm -f moccha
	rm -f cmd/server/ngrok

run:
	./moccha -port 8080 -token mysecret

run-ngrok:
	./moccha -ngrok -port 8080 -token mysecret
