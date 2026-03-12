#!/bin/bash
# Download ngrok binary for Moccha
# Usage: ./scripts/download-ngrok.sh

set -e

NGROK_VERSION="3.8.0"
NGROK_DIR="cmd/server"
NGROK_FILE="$NGROK_DIR/ngrok"

echo "Downloading ngrok v$NGROK_VERSION..."

# Try different download methods
if command -v curl &> /dev/null; then
    curl -fsSL "https://github.com/ngrok/ngrok/releases/download/v${NGROK_VERSION}/ngrok-v${NGROK_VERSION}-linux-amd64.zip" -o /tmp/ngrok.zip
elif command -v wget &> /dev/null; then
    wget -q "https://github.com/ngrok/ngrok/releases/download/v${NGROK_VERSION}/ngrok-v${NGROK_VERSION}-linux-amd64.zip" -O /tmp/ngrok.zip
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
