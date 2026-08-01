#!/bin/bash
# 构建 Linux amd64 最小化二进制
#
# 用法: ./build.sh
# 输出: build/icloud-hme

set -e

OUTPUT_DIR="build"
BINARY_NAME="icloud-hme"
VERSION="${VERSION:-dev}"

echo "==> 清理旧的构建文件"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

echo "==> 安装前端依赖并构建静态资源"
npm --prefix web ci
npm --prefix web run build

echo "==> 准备 Go 嵌入的前端静态资源"
rm -rf internal/webui/dist/assets internal/webui/dist/index.html
cp -R web/dist/. internal/webui/dist/

echo "==> 构建 Linux amd64 最小化二进制 (version: $VERSION)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -buildvcs=false -trimpath \
    -ldflags="-s -w -buildid= -X main.version=${VERSION}" \
    -gcflags="-l=4" \
    -o "$OUTPUT_DIR/$BINARY_NAME" \
    .

echo "==> 压缩二进制 (upx)"
if command -v upx >/dev/null 2>&1; then
  upx --best --lzma "$OUTPUT_DIR/$BINARY_NAME" || true
else
  echo "    (upx 未安装,跳过压缩)"
fi

echo ""
echo "==> 构建完成"
echo "    文件: $OUTPUT_DIR/$BINARY_NAME"
ls -lh "$OUTPUT_DIR/$BINARY_NAME"
