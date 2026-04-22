FROM alpine:latest

# 1. 安装基础运行依赖：时区和证书是必须的
RUN apk add --no-cache tzdata ca-certificates

WORKDIR /

# 2. 拷贝宿主机编译好的产物
# 确保你的构建脚本中 go build 的输出文件名是 casdoor-bin
COPY casdoor-bin /server
COPY web/build /web/build
# 即使配置在 cfgserver，Casdoor 启动仍需读取 app.conf 模板
COPY conf/app.conf /conf/app.conf

# 3. 保险起见，创建 logs 目录并保持 root 权限
RUN mkdir -p logs

# 4. 暴露 Casdoor 默认端口
EXPOSE 8000

# 5. 启动程序
ENTRYPOINT ["/server"]
