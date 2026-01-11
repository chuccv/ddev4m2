#!/usr/bin/env bash
# Script để cài đặt ddev4m2 từ binary sẵn (KHÔNG build)
# Usage: curl -fsSL https://raw.githubusercontent.com/chuccv/ddev4m2/main/install_ddev4m2.sh | bash

set -e

DDEV_GITHUB_OWNER=${DDEV_GITHUB_OWNER:-chuccv}
DDEV_REPO=${DDEV_REPO:-ddev4m2}
VERSION=${1:-v2.24.10}

RED='\033[31m'
GREEN='\033[32m'
YELLOW='\033[33m'
RESET='\033[0m'

if [[ $EUID -eq 0 ]]; then
    echo "Script này không nên chạy với sudo/root." >&2
    exit 102
fi

unamearch=$(uname -m)
case ${unamearch} in
  x86_64) ARCH="amd64";;
  aarch64) ARCH="arm64";;
  arm64) ARCH="arm64";;
  *) printf "${RED}Kiến trúc ${unamearch} chưa được hỗ trợ.${RESET}\n" && exit 106;;
esac

if [ ! -d /usr/local/bin ]; then
    sudo mkdir -p /usr/local/bin
fi

BINOWNER=$(ls -ld /usr/local/bin | awk '{print $3}')
USER=$(whoami)
SUDO=""

if [[ "$BINOWNER" != "$USER" ]]; then
    SUDO=sudo
fi

if ! docker --version >/dev/null 2>&1; then
    printf "${YELLOW}Docker chưa được cài đặt.${RESET}\n"
fi

RELEASE_BASE_URL="https://github.com/${DDEV_GITHUB_OWNER}/${DDEV_REPO}/releases/download/${VERSION}"

printf "${GREEN}Đang tải ddev từ binary (KHÔNG build)...${RESET}\n"
printf "${YELLOW}URL: ${RELEASE_BASE_URL}/ddev${RESET}\n"
$SUDO curl -fsSL "${RELEASE_BASE_URL}/ddev" -o /usr/local/bin/ddev || {
    printf "${RED}Không thể tải ddev từ release.${RESET}\n"
    printf "${RED}Vui lòng kiểm tra release: https://github.com/${DDEV_GITHUB_OWNER}/${DDEV_REPO}/releases/tag/${VERSION}${RESET}\n"
    exit 1
}

$SUDO curl -fsSL "${RELEASE_BASE_URL}/ddev-hostname" -o /usr/local/bin/ddev-hostname 2>/dev/null || true

$SUDO chmod +x /usr/local/bin/ddev
$SUDO chmod +x /usr/local/bin/ddev-hostname 2>/dev/null || true

hash -r

printf "${GREEN}✅ DDEV đã được cài đặt thành công!${RESET}\n"
printf "${GREEN}Chạy 'ddev version' để kiểm tra${RESET}\n"
