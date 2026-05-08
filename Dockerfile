# 第一阶段：编译环境 (已将基础镜像升级为最新的 Go 版本以兼容依赖库)
FROM golang:alpine AS builder

# 安装 git，这是拉取 GitHub 第三方包必需的
RUN apk add --no-cache git

WORKDIR /app

# 复制 main.go 进容器
COPY main.go .

# 配置 Go 代理，防止网络问题导致拉取失败
ENV GOPROXY=https://goproxy.cn,direct

# 初始化模块并下载依赖
RUN go mod init custom-doh && go mod tidy

# 编译成静态链接的二进制文件
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ecs-doh-server main.go

# 第二阶段：运行环境
FROM alpine:latest

# 安装根证书（请求 Google DoH 必须）
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# 从 builder 阶段复制二进制文件
COPY --from=builder /app/ecs-doh-server .

EXPOSE 8080

CMD ["./ecs-doh-server"]
