#!/bin/bash
set -e

# 配置变量
IMAGE_NAME="casdoor:cftp"
TAR_NAME="casdoor_cftp.tar"
BINARY_NAME="casdoor-bin"

echo "==== Step 1: Building Frontend (React) ===="
cd web
# 如果你没有安装 yarn，可以改为 npm install && npm run build
yarn install
yarn build
cd ..

echo "==== Step 2: Building Backend (Go) ===="
# 清理旧的二进制
rm -f ./$BINARY_NAME

# 静态编译，注入你的修改
# GOARCH=amd64 (如果你是在平板上运行，请改为 arm64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $BINARY_NAME .

echo "==== Step 3: Building Image with Buildah ===="
# 使用我们自定义的 cftp.Dockerfile
buildah build -f cftp.Dockerfile -t $IMAGE_NAME .

echo "==== Step 4: Exporting and Importing to K3s ===="
# 导出为 docker-archive 格式
rm -f $TAR_NAME
buildah push --format docker $IMAGE_NAME docker-archive:$TAR_NAME

# 导入到 k3s 内部存储 (containerd)
sudo k3s ctr images import $TAR_NAME

# 清理临时 tar 包
rm $TAR_NAME

echo "==== Finished! ===="
echo "Image $IMAGE_NAME is now available in K3s."
sudo k3s ctr images ls | grep $IMAGE_NAME