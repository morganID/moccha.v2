.PHONY: all build build-darwin-arm64 build-linux-amd64 clean run run-debug run-prod run-ngrok

all: build-darwin-arm64 build-linux-amd64

# Build for current platform
build:
	go build -o moccha ./cmd/server

# Build for Mac M1 (Apple Silicon)
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o moccha-darwin-arm64 ./cmd/server

# Build for Linux (x86_64)
build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o moccha-linux-amd64 ./cmd/server

clean:
	rm -f moccha
	rm -f moccha-darwin-arm64 moccha-linux-amd64

run:
	./moccha -port 8080 -token mysecret

run-debug:
	./moccha -debug -port 8080 -token mysecret

run-prod:
	./moccha -port 8080 -token mysecret

run-ngrok:
	./moccha -ngrok -port 8080 -token mysecret
