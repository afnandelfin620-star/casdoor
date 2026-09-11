#!/usr/bin/env bash
set -e

# clean old binary
BINARY_NAME="casdoor-bin"
rm -f ./$BINARY_NAME

# Parameter: environment or tag (defaults to dev)
# Usage: ./cftp-build.sh [dev|prod|<custom-tag>]
ENV_OR_TAG=${1:-dev}
if [ "$ENV_OR_TAG" = "dev" ] || [ "$ENV_OR_TAG" = "prod" ]; then
  TAG="${ENV_OR_TAG}-latest"
else
  TAG="$ENV_OR_TAG"
fi

# Detect CPU architecture for Go (supports manual GOARCH override)
if [ -z "$GOARCH" ]; then
  case "$(uname -m)" in
    x86_64)
      GOARCH="amd64"
      ;;
    aarch64|arm64)
      GOARCH="arm64"
      ;;
    *)
      GOARCH="$(go env GOHOSTARCH 2>/dev/null || echo amd64)"
      ;;
  esac
fi

echo ">>> Building casdoor [Tag: ${TAG}] [Arch: ${GOARCH}]..."

echo "==== Step 1: Building Frontend (React) ===="
cd web

export GENERATE_SOURCEMAP=false
export NODE_OPTIONS="--max-old-space-size=4096"
# 如果你没有安装 yarn，可以改为 npm install && npm run build
yarn install
yarn build
cd ..

# --- 关键修正：确保目录名匹配 Dockerfile ---
if [ -d "web/build-temp" ]; then
  echo "Renaming build-temp to build..."
  rm -rf web/build
  mv web/build-temp web/build
fi

echo "==== Step 2: Building Backend (Go) ===="
# 静态编译
# CGO_ENABLED=0 ensures static compilation
# -ldflags="-s -w" shrinks binary size by stripping debug symbols
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" go build -ldflags="-s -w" -o $BINARY_NAME .

echo "==== Step 3: Building Image [Tag: ${TAG}] [Arch: ${GOARCH}] ===="
# Build image
if command -v docker >/dev/null 2>&1; then
  echo ">>> Building image directly with Docker..."
  if docker info >/dev/null 2>&1; then
    docker build -f cftp.Dockerfile -t "casdoor:${TAG}" -t "localhost/casdoor:${TAG}" .
  else
    sudo docker build -f cftp.Dockerfile -t "casdoor:${TAG}" -t "localhost/casdoor:${TAG}" .
  fi
  echo ">>> casdoor:${TAG} built and ready for Docker Compose!"
elif command -v buildah >/dev/null 2>&1; then
  echo ">>> Docker not found, fallback to buildah..."
  buildah build -f cftp.Dockerfile -t "casdoor:${TAG}" .
  if command -v k3s >/dev/null 2>&1; then
    buildah push --format docker "casdoor:${TAG}" "docker-archive:casdoor.tar"
    sudo k3s ctr images import casdoor.tar
    sudo k3s ctr images ls | grep casdoor
    rm -f casdoor.tar
    echo ">>> casdoor:${TAG} imported into K3s successfully!"
  fi
else
  echo ">>> Error: Neither Docker nor buildah found on this system!"
  exit 1
fi

echo "==== Finished! ===="
echo "Image casdoor:${TAG} built successfully."
