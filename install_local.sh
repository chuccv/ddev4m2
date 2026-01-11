#!/usr/bin/env bash
# Script để cài đặt ddev từ binary đã build local

set -e

DDEV_BUILD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DDEV_BIN_DIR="$DDEV_BUILD_DIR/.gotmp/bin/linux_amd64"

if [ ! -f "$DDEV_BIN_DIR/ddev" ]; then
    echo "Error: Binary ddev không tìm thấy tại $DDEV_BIN_DIR/ddev"
    echo "Vui lòng chạy 'make' trước để build ddev"
    exit 1
fi

if [ ! -d /usr/local/bin ]; then
    echo "Tạo /usr/local/bin..."
    sudo mkdir -p /usr/local/bin
fi

BINOWNER=$(ls -ld /usr/local/bin | awk '{print $3}')
USER=$(whoami)
SUDO=""

if [[ "$BINOWNER" != "$USER" ]]; then
    SUDO=sudo
fi

echo "Cài đặt ddev vào /usr/local/bin..."
$SUDO cp -f "$DDEV_BIN_DIR/ddev" /usr/local/bin/ddev
$SUDO cp -f "$DDEV_BIN_DIR/ddev-hostname" /usr/local/bin/ddev-hostname 2>/dev/null || true
$SUDO chmod +x /usr/local/bin/ddev
$SUDO chmod +x /usr/local/bin/ddev-hostname 2>/dev/null || true

hash -r

echo "✅ DDEV đã được cài đặt thành công!"
echo "Chạy 'ddev version' để kiểm tra"
