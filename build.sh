#!/bin/bash
set -e

BINARY_NAME="git_mcp"
TARGET_DIR="."
INSTALL_DIR="/usr/local/bin"

echo "🐹 Building $BINARY_NAME (Release Mode)..."
go build -ldflags="-s -w" -o "$BINARY_NAME"

echo "📦 Installing to $INSTALL_DIR..."
if [ -w "$INSTALL_DIR" ]; then
    cp "$BINARY_NAME" "$INSTALL_DIR/"
else
    sudo cp "$BINARY_NAME" "$INSTALL_DIR/"
fi

echo "✅ Success! Installed"
