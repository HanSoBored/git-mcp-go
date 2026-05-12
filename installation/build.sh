#!/bin/bash
set -e

BINARY_NAME="git_mcp"
BUILD_DIR="../build"
INSTALL_DIR="/usr/local/bin"

# --- Sudo Configuration ---
: ${USE_SUDO:="true"}

# Get script directory to find VERSION file
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
VERSION_FILE="$PROJECT_ROOT/VERSION"

# Parse command-line flags
while [[ $# -gt 0 ]]; do
    case $1 in
        '--no-sudo') USE_SUDO="false"; shift ;;
        *) shift ;;
    esac
done

# runAsRoot executes a command with sudo if needed, with user warning
runAsRoot() {
    if [ "$EUID" -ne 0 ] && [ "$USE_SUDO" = "true" ]; then
        echo "🔑 Sudo required to install to $INSTALL_DIR"
        sudo "${@}"
    else
        "${@}"
    fi
}

# Read version from VERSION file
if [ -f "$VERSION_FILE" ]; then
    VERSION=$(cat "$VERSION_FILE")
    echo "📋 Version: $VERSION"
else
    VERSION="0.0.0-dev"
    echo "⚠️  VERSION file not found, using: $VERSION"
fi

echo "🐹 Building $BINARY_NAME (Release Mode)..."
mkdir -p "$BUILD_DIR"
cd src/
go build -ldflags="-s -w -X main.version=$VERSION" -o "$BUILD_DIR/$BINARY_NAME"

echo "📦 Installing to $INSTALL_DIR..."
runAsRoot cp "$BUILD_DIR/$BINARY_NAME" "$INSTALL_DIR/"

echo "✅ Success! Installed"
