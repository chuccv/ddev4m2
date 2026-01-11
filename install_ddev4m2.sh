#!/usr/bin/env bash
# Script để cài đặt ddev4m2 (DDEV tối ưu cho Magento/PHP)
# Usage: curl -fsSL https://raw.githubusercontent.com/chuccv/ddev4m2/main/install_ddev4m2.sh | bash

set -e

DDEV_GITHUB_OWNER=${DDEV_GITHUB_OWNER:-chuccv}
DDEV_REPO=${DDEV_REPO:-ddev4m2}
DDEV_BRANCH=${DDEV_BRANCH:-main}

RED='\033[31m'
GREEN='\033[32m'
YELLOW='\033[33m'
RESET='\033[0m'

if [[ $EUID -eq 0 ]]; then
    echo "Script này không nên chạy với sudo/root. Vui lòng chạy lại không có sudo." >&2
    exit 102
fi

if ! command -v git &> /dev/null; then
    printf "${RED}Git không được cài đặt. Vui lòng cài đặt git trước.${RESET}\n"
    exit 1
fi

if ! command -v go &> /dev/null; then
    printf "${RED}Go không được cài đặt. Vui lòng cài đặt Go trước.${RESET}\n"
    printf "${YELLOW}Xem hướng dẫn: https://go.dev/doc/install${RESET}\n"
    exit 1
fi

if ! docker --version >/dev/null 2>&1; then
    printf "${YELLOW}Docker không được cài đặt. Vui lòng cài đặt Docker trước.${RESET}\n"
    printf "${YELLOW}Xem hướng dẫn: https://docs.ddev.com/en/stable/users/install/docker-installation/${RESET}\n"
fi

TMPDIR=/tmp/ddev4m2_install
REPO_DIR="$TMPDIR/$DDEV_REPO"

printf "${GREEN}Đang clone repository...${RESET}\n"
rm -rf "$REPO_DIR"
mkdir -p "$TMPDIR"
git clone --depth 1 --branch "$DDEV_BRANCH" "https://github.com/${DDEV_GITHUB_OWNER}/${DDEV_REPO}.git" "$REPO_DIR"

cd "$REPO_DIR"

printf "${GREEN}Đang build ddev...${RESET}\n"
make || {
    printf "${RED}Build thất bại. Vui lòng kiểm tra lỗi ở trên.${RESET}\n"
    exit 1
}

if [ ! -d /usr/local/bin ]; then
    printf "${YELLOW}Tạo /usr/local/bin...${RESET}\n"
    sudo mkdir -p /usr/local/bin
fi

BINOWNER=$(ls -ld /usr/local/bin | awk '{print $3}')
USER=$(whoami)
SUDO=""

if [[ "$BINOWNER" != "$USER" ]]; then
    SUDO=sudo
fi

ARCH=$(uname -m)
case ${ARCH} in
    x86_64) BIN_ARCH="linux_amd64";;
    aarch64|arm64) BIN_ARCH="linux_arm64";;
    *) printf "${RED}Kiến trúc ${ARCH} chưa được hỗ trợ.${RESET}\n" && exit 1;;
esac

BIN_DIR=".gotmp/bin/${BIN_ARCH}"

if [ ! -f "$BIN_DIR/ddev" ]; then
    printf "${RED}Binary ddev không tìm thấy tại $BIN_DIR/ddev${RESET}\n"
    exit 1
fi

printf "${GREEN}Đang cài đặt ddev vào /usr/local/bin...${RESET}\n"
$SUDO cp -f "$BIN_DIR/ddev" /usr/local/bin/ddev
$SUDO cp -f "$BIN_DIR/ddev-hostname" /usr/local/bin/ddev-hostname 2>/dev/null || true
$SUDO chmod +x /usr/local/bin/ddev
$SUDO chmod +x /usr/local/bin/ddev-hostname 2>/dev/null || true

hash -r

printf "${GREEN}✅ DDEV đã được cài đặt thành công!${RESET}\n"
printf "${GREEN}Chạy 'ddev version' để kiểm tra${RESET}\n"

rm -rf "$TMPDIR"
