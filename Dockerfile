# 第一阶段：编译环境
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 只需要将 main.go 复制进去
COPY main.go .

# 直接在 Docker 容器内初始化 Go 模块并拉取依赖（省去本地操作）
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
