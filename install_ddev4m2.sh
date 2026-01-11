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

OS=$(uname)
if [[ "$OS" == "Linux" ]]; then
    FILEBASE="ddev_linux-${ARCH}"
elif [[ "$OS" == "Darwin" ]]; then
    FILEBASE="ddev_macos-${ARCH}"
else
    printf "${RED}Platform ${OS} chưa được hỗ trợ.${RESET}\n"
    exit 1
fi

TMPDIR=/tmp
TARBALL="${FILEBASE}.${VERSION}.tar.gz"
RELEASE_BASE_URL="https://github.com/${DDEV_GITHUB_OWNER}/${DDEV_REPO}/releases/download/${VERSION}"

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

printf "${GREEN}Đang tải binary từ release...${RESET}\n"
curl -fsSL "${RELEASE_BASE_URL}/${TARBALL}" -o "${TMPDIR}/${TARBALL}" || {
    printf "${RED}Không thể tải binary từ release.${RESET}\n"
    printf "${YELLOW}Vui lòng dùng script build: install_ddev4m2.sh${RESET}\n"
    exit 1
}

cd "${TMPDIR}"
tar -xzf "${TARBALL}"

printf "${GREEN}Đang cài đặt...${RESET}\n"
$SUDO cp -f "${FILEBASE}/ddev" /usr/local/bin/ddev
$SUDO cp -f "${FILEBASE}/ddev-hostname" /usr/local/bin/ddev-hostname 2>/dev/null || true
$SUDO chmod +x /usr/local/bin/ddev
$SUDO chmod +x /usr/local/bin/ddev-hostname 2>/dev/null || true

hash -r
rm -rf "${TMPDIR}/${TARBALL}" "${TMPDIR}/${FILEBASE}"

printf "${GREEN}✅ DDEV đã được cài đặt thành công!${RESET}\n"
printf "${GREEN}Chạy 'ddev version' để kiểm tra${RESET}\n"
