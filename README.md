# Custom DoH Server: CDN 穿透与 ECS 注入中继

这是一个基于 Golang 编写的高性能 DoH (DNS over HTTPS) 中继服务器。专为部署在 CDN（如 Cloudflare, Tencent EdgeOne, Fastly 等）背后的场景设计。

它能够精准提取穿透 CDN 的客户端真实 IP（完美支持 IPv4 & IPv6），将其制作成 **EDNS Client Subnet (ECS)** 附加记录，并安全地转发给上游 DNS（如 Google DoH）。从而彻底解决 DoH 服务套用 CDN 后，上游 DNS 无法进行精准 Geo 调度的问题。

## ✨ 核心特性

* 🚀 **精准的真实 IP 提取**：自动解析 `CF-Connecting-IP`, `EO-Client-IP`, `Fastly-Client-IP`, `X-Real-IP` 等专属 Header。
* 🎯 **智能 ECS 注入**：强力拦截并重写 DNS 报文，注入真实用户的 IP 子网（IPv4 默认 `/24` 掩码，IPv6 默认 `/56` 掩码），在保证极致解析准确率的同时保护用户隐私。
* 🛡️ **私有路径防白嫖**：支持通过环境变量自定义复杂的访问路径，有效防御全网扫描器，避免节点沦为公共代理池。
* 🐳 **极简多架构部署**：提供构建好的 Docker 镜像，原生支持 `amd64` (x86) 与 `arm64` (如 Oracle ARM, Mac M1/M2, 树莓派) 架构。
* ⚡ **极低资源开销**：采用 Go 原生极简架构，结合 Docker 多阶段构建，运行内存仅需几 MB。

---

## 🚀 快速部署 (Docker Compose)

推荐使用 Docker Compose 进行部署，你可以轻松地将其与 Nginx反向代理或 1Panel 面板结合使用。

创建一个 `docker-compose.yml` 文件：

```yaml
version: '3.8'

services:
  custom-doh:
    # 请替换为你实际的 GitHub Packages 镜像地址
    image: ghcr.io/jarmohu/doh-cdn-ecs:main 
    container_name: custom-doh
    restart: always
    environment:
      # 【极其重要】设置你的私有 DoH 路径，必须以 "/" 开头
      - DOH_PATH=/my-secret-doh-path-9988
    ports:
      # 建议绑定在本地 127.0.0.1，然后使用 Nginx/OpenResty 反代并配置 HTTPS
      - "127.0.0.1:8080:8080"

```

启动服务：

```bash
docker-compose up -d

```

*如果你使用 1Panel 面板，可以直接在“容器 -> 编排”中粘贴上述 YAML 并将网络接入 `1panel-network`。*

---

## 🛠️ 配置 Nginx / CDN

为了使服务正常工作，你**必须**在服务前端配置一个带有有效 TLS 证书的 HTTPS 反向代理，并将其接入支持的 CDN。

客户端的完整请求链路应为：
`客户端 -> CDN (Cloudflare/EdgeOne) -> Nginx (反向代理) -> 本项目 (Docker:8080) -> Google DoH`

你的最终可用 DoH 地址格式将类似于：
`https://dns.yourdomain.com/my-secret-doh-path-9988`

---

## 🧪 验证与测试

服务启动并配置好域名后，你可以使用支持 DoH 的现代 DNS 测试工具（如 `doggo`）进行验证：

```bash
# 测试基础解析能力
doggo A google.com @https://dns.yourdomain.com/my-secret-doh-path-9988 --transport doh

```

**验证 ECS 注入是否成功：**
由于测试工具本身的 IP 可能与服务器 IP 相同，建议通过伪造 HTTP Header 的方式进行高级测试。你可以向节点发送带有特定 `CF-Connecting-IP` 的请求，并查询 `o-o.myaddr.l.google.com` (TXT 记录)，如果返回结果包含了你伪造的 IP 网段，则证明穿透与注入完美生效。

---

## 🏗️ 自动化构建 (针对开发者)

本项目自带完整的 GitHub Actions CI/CD 工作流。

1. Fork 本仓库。
2. 任何提交推送到 `main` 分支时，Actions 将自动触发。
3. 自动使用 QEMU 和 Docker Buildx 交叉编译生成包含 `amd64` 和 `arm64` 的混合架构镜像。
4. 镜像将自动发布到你的 GitHub Container Registry (GHCR)。

> **注意：** 首次构建完成后，请前往 GitHub 个人主页的 `Packages` 页面，将镜像的可见性（Visibility）更改为 `Public`，以便服务器可以免密拉取。
