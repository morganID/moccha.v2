#!/bin/bash
# Download ngrok binary for Moccha
# Usage: ./scripts/download-ngrok.sh

set -e

NGROK_VERSION="3.8.0"
NGROK_DIR="cmd/server"
NGROK_FILE="$NGROK_DIR/ngrok"

echo "Downloading ngrok v$NGROK_VERSION..."

# Detect OS and select appropriate ngrok binary
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture
if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "arm64" ] || [ "$ARCH" = "aarch64" ]; then
    ARCH="arm64"
fi

# Build download URL
if [ "$OS" = "darwin" ]; then
    DOWNLOAD_URL="https://github.com/ngrok/ngrok/releases/download/v${NGROK_VERSION}/ngrok-v${NGROK_VERSION}-darwin-${ARCH}.zip"
elif [ "$OS" = "linux" ]; then
    DOWNLOAD_URL="https://github.com/ngrok/ngrok/releases/download/v${NGROK_VERSION}/ngrok-v${NGROK_VERSION}-linux-${ARCH}.zip"
else
    echo "Error: Unsupported OS: $OS"
    exit 1
fi

echo "Detected OS: $OS, Architecture: $ARCH"
echo "Download URL: $DOWNLOAD_URL"

# Try different download methods
if command -v curl &> /dev/null; then
    curl -fsSL "$DOWNLOAD_URL" -o /tmp/ngrok.zip
elif command -v wget &> /dev/null; then
    wget -q "$DOWNLOAD_URL" -O /tmp/ngrok.zip
else
    echo "Error: curl or wget required"
    exit 1
fi

echo "Extracting..."
unzip -o /tmp/ngrok.zip -d "$NGROK_DIR"
chmod +x "$NGROK_FILE"
rm -f /tmp/ngrok.zip

echo "Done! ngrok installed at $NGROK_FILE"
echo ""
echo "To rebuild with ngrok embedded:"
echo "  go build -o moccha ./cmd/server"
