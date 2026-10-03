#!/bin/bash
# Cross-platform build script for TermChat
# Builds binaries for Linux, macOS, and Windows

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="$PROJECT_ROOT/bin"
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

# Ensure dist directory exists
mkdir -p "$OUTPUT_DIR"

echo "🔨 Building TermChat cross-platform binaries..."
echo "Version: $VERSION"
echo "Output: $OUTPUT_DIR"
echo ""

# Define build targets: (GOOS GOARCH output_name)
# Note: For Android, use the linux/arm64 build with Termux (Linux environment on Android)
# Native Android NDK builds (android/arm64) require the Android NDK toolchain.
TARGETS=(
  "linux amd64 termchat-linux-amd64"
  "linux arm64 termchat-linux-arm64"
  "darwin amd64 termchat-macos-amd64"
  "darwin arm64 termchat-macos-arm64"
  "windows amd64 termchat-windows-amd64.exe"
  "windows arm64 termchat-windows-arm64.exe"
)

# Build each target
for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH OUTPUT <<< "$target"
  OUTPUT_PATH="$OUTPUT_DIR/$OUTPUT"

  echo "📦 Building $GOOS/$GOARCH client → $OUTPUT"
  GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -ldflags "-s -w -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" \
    -o "$OUTPUT_PATH" \
    ./cmd/client

  # Also build the server
  SERVER_OUTPUT="${OUTPUT/termchat/termchat-server}"
  SERVER_OUTPUT_PATH="$OUTPUT_DIR/$SERVER_OUTPUT"
  echo "📦 Building $GOOS/$GOARCH server → $SERVER_OUTPUT"
  GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -ldflags "-s -w -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" \
    -o "$SERVER_OUTPUT_PATH" \
    ./cmd/server

  # Make executable on Unix systems
  if [[ "$GOOS" != "windows" ]]; then
    chmod +x "$OUTPUT_PATH"
    chmod +x "$SERVER_OUTPUT_PATH"
  fi

  # Show file size
  size=$(du -h "$OUTPUT_PATH" | cut -f1)
  echo "   ✓ Size: $size"
done

# Keep the historical Linux launcher names in sync with the release build.
cp "$OUTPUT_DIR/termchat-linux-amd64" "$OUTPUT_DIR/termchat"
cp "$OUTPUT_DIR/termchat-linux-amd64" "$OUTPUT_DIR/termchat-linux"
cp "$OUTPUT_DIR/termchat-windows-amd64.exe" "$OUTPUT_DIR/termchat.exe"

echo ""
echo "✅ Build complete!"
echo ""
echo "📍 Binaries location: $OUTPUT_DIR"
echo ""
echo "🚀 Usage:"
echo "   Linux/Mac:  ./termchat-linux-amd64 -server <SERVER_URL>"
echo "   Windows:    termchat-windows-amd64.exe -server <SERVER_URL>"
echo ""
echo "💡 Example with Cloudflare:"
echo "   ./termchat-linux-amd64 -server wss://termchat-relay.meetkhamar3501.workers.dev/ws"
echo ""
