#!/usr/bin/env bash
set -euo pipefail

# 滚动版本发布脚本
# 用法: ./build.sh [patch|minor|major]
# 功能: 自动递增版本号，将当前所有本地改动与版本号合并为一次发布提交，推送到 main 触发自动部署，然后创建并推送版本 tag

VERSION_BUMP="${1:-patch}"

echo "=== 滚动版本发布 ==="
echo ""

# 检查是否在 main 分支
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo "错误: 当前不在 main 分支，请先切换到 main 分支"
  exit 1
fi

# 获取当前版本
CURRENT_VERSION=$(node -p "require('./package.json').version")
echo "当前版本: v${CURRENT_VERSION}"

# 递增版本
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"
case "$VERSION_BUMP" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch|*)
    PATCH=$((PATCH + 1))
    ;;
esac
NEW_VERSION="${MAJOR}.${MINOR}.${PATCH}"
TAG_NAME="v${NEW_VERSION}"
echo "新版本:   v${NEW_VERSION}"
echo ""

if git rev-parse "${TAG_NAME}" >/dev/null 2>&1; then
  echo "错误: 本地已存在标签 ${TAG_NAME}"
  exit 1
fi

if git ls-remote --tags origin "refs/tags/${TAG_NAME}" | grep -q "${TAG_NAME}"; then
  echo "错误: 远程已存在标签 ${TAG_NAME}"
  exit 1
fi

# 更新 package.json
node -e "
const pkg = require('./package.json');
pkg.version = '${NEW_VERSION}';
require('fs').writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
"

# 提交并推送
echo "提交本地改动和版本变更..."
git add -A
git commit -m "chore: release v${NEW_VERSION}"

echo "推送代码到远程（跳过本次 main 流水线，后续由 tag 流水线负责发布）..."
git push -o ci.skip origin main

echo "创建发布标签..."
git tag -a "${TAG_NAME}" -m "Release ${TAG_NAME}"

echo "推送发布标签..."
git push origin "${TAG_NAME}"

echo ""
echo "推送完成！已跳过本次 main 流水线，仅由版本标签 ${TAG_NAME} 触发一次发布流水线。"
echo "查看进度: https://gitlab.com/-/pipelines/"
