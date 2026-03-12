.PHONY: all build embed-ngrok clean run

all: build

build:
	go build -o moccha ./cmd/server

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
