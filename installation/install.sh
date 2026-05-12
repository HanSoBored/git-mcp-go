#!/bin/bash
set -e

# --- CONFIGURATION ---
REPO_OWNER="HanSoBored"
REPO_NAME="git-mcp-go"
BINARY_BASE_NAME="git-mcp-go"
FINAL_NAME="git_mcp"
INSTALL_DIR="/usr/local/bin"

# --- Checksum Verification Configuration ---
: ${VERIFY_CHECKSUM:="true"}

# Parse command-line flags
while [[ $# -gt 0 ]]; do
    case $1 in
        '--skip-verify') VERIFY_CHECKSUM="false"; shift ;;
        *) shift ;;
    esac
done

# --- DETECT SYSTEM ---
OS="$(uname -s)"
ARCH="$(uname -m)"

echo "🔍 Detecting system..."
echo "   OS: $OS"
echo "   Arch: $ARCH"

SUFFIX=""

# 1. DETECT OS & MAP ARCHITECTURE
if [ "$OS" = "Linux" ]; then
    if [ "$ARCH" = "x86_64" ]; then
        SUFFIX="linux-x86_64"
    elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        SUFFIX="linux-aarch64"
    elif [[ "$ARCH" == armv7* ]] || [ "$ARCH" = "arm" ]; then
        SUFFIX="linux-armv7"
    else
        echo "❌ Unsupported Architecture: $ARCH on Linux"
        exit 1
    fi
elif [ "$OS" = "Darwin" ]; then
    if [ "$ARCH" = "x86_64" ]; then
        SUFFIX="darwin-x86_64"
    elif [ "$ARCH" = "arm64" ]; then
        # macOS returns 'arm64' for M1/M2, but we named the file 'aarch64'
        SUFFIX="darwin-aarch64"
    else
        echo "❌ Unsupported Architecture: $ARCH on macOS"
        exit 1
    fi
else
    echo "❌ Unsupported OS: $OS"
    exit 1
fi

TARGET_FILE="${BINARY_BASE_NAME}-${SUFFIX}"
echo "🎯 Target Release Asset: $TARGET_FILE"

# --- DOWNLOADING ---
echo "⬇️  Downloading latest release..."
DOWNLOAD_URL="https://github.com/$REPO_OWNER/$REPO_NAME/releases/latest/download/$TARGET_FILE"

# Use curl to download to temp folder
# -L follows redirects
# -f fails silently on server error (404) so we can catch it
if ! curl -f -L -o "/tmp/$BINARY_BASE_NAME" "$DOWNLOAD_URL"; then
    echo "❌ Error: Failed to download. The release asset '$TARGET_FILE' might not exist yet."
    exit 1
fi

# --- VERIFYING ---
if [ "$VERIFY_CHECKSUM" = "true" ]; then
    echo "🔐 Verifying SHA256 checksum..."
    CHECKSUM_URL="https://github.com/$REPO_OWNER/$REPO_NAME/releases/latest/download/${TARGET_FILE}.sha256"

    # Download expected checksum
    if ! curl -f -L -o "/tmp/${BINARY_BASE_NAME}.sha256" "$CHECKSUM_URL" 2>/dev/null; then
        echo "❌ Error: SHA256 checksum file not found"
        echo "   To skip verification (NOT RECOMMENDED), use: --skip-verify"
        rm -f "/tmp/$BINARY_BASE_NAME"
        exit 1
    fi
    
    # Extract expected hash from checksum file (format: "hash  filename" or "hash filename")
    EXPECTED_HASH=$(awk '{print $1}' "/tmp/${BINARY_BASE_NAME}.sha256")
    
    # Calculate actual hash
    if command -v sha256sum &> /dev/null; then
        ACTUAL_HASH=$(sha256sum "/tmp/$BINARY_BASE_NAME" | awk '{print $1}')
    elif command -v shasum &> /dev/null; then
        ACTUAL_HASH=$(shasum -a 256 "/tmp/$BINARY_BASE_NAME" | awk '{print $1}')
    else
        echo "❌ Error: No SHA256 tool found (sha256sum or shasum required)"
        exit 1
    fi
    
    if [ "$EXPECTED_HASH" != "$ACTUAL_HASH" ]; then
        echo "❌ Error: SHA256 checksum mismatch!"
        echo "   Expected: $EXPECTED_HASH"
        echo "   Actual:   $ACTUAL_HASH"
        echo "   Binary may be corrupted or tampered with."
        rm -f "/tmp/$BINARY_BASE_NAME" "/tmp/${BINARY_BASE_NAME}.sha256"
        exit 1
    fi
    
    echo "✅ Checksum verified successfully"
    rm -f "/tmp/${BINARY_BASE_NAME}.sha256"
else
    echo "⚠️  Skipping SHA256 verification (--skip-verify)"
fi

# --- INSTALLING ---
echo "📦 Installing to $INSTALL_DIR..."
chmod +x "/tmp/$BINARY_BASE_NAME"

# Check write permissions
if [ -w "$INSTALL_DIR" ]; then
    mv "/tmp/$BINARY_BASE_NAME" "$INSTALL_DIR/$FINAL_NAME"
else
    echo "🔑 Sudo permission required to move binary to $INSTALL_DIR"
    sudo mv "/tmp/$BINARY_BASE_NAME" "$INSTALL_DIR/$FINAL_NAME"
fi

echo "✅ Installed successfully!"
echo "   Binary location: $INSTALL_DIR/$FINAL_NAME"
echo "   You can now run it using: $FINAL_NAME"
