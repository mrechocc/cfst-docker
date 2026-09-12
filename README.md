# CloudflareSpeedTest Docker 测试器

本项目把 [XIU2/CloudflareSpeedTest](https://github.com/XIU2/CloudflareSpeedTest) 固定在 `v2.3.5` 源码标签，并在 Docker 构建阶段编译。测速二进制不会安装到宿主机；宿主机只保存 `results/` 下的 CSV 结果。

这个项目只测试，不读取 DNS 凭证、不修改 DNS、不提供代理或反代服务。

## 前提

- Linux VPS 或 Linux 主机，已安装 Docker Engine 和 Docker Compose 插件。
- `docker compose version` 可以正常执行。
- Docker 使用 host 网络模式，以尽量贴近 VPS 自身的实际出站网络；容器不监听或开放端口。此模式不适用于 Windows/macOS Docker Desktop。
- 测试前关闭会改变出站路径的代理/VPN。上游项目也指出，若测速经过代理，结果会不准确。

## 运行

### 从 Windows 上传到 VPS

在 Windows PowerShell 中执行。将 `<vps-user>` 和 `<vps-ip>` 换成 VPS 的 SSH 用户名和 IP；使用普通用户目录，不要把项目上传到系统目录。

```powershell
scp -r "D:\AI\cf优选IP\cfst-docker" <vps-user>@<vps-ip>:~/
ssh <vps-user>@<vps-ip>
```

登录 VPS 后先确认系统和 CPU 架构：

```bash
cat /etc/os-release
uname -m
```

若未安装 Docker Engine 和 Compose 插件，请按照 Docker 官方与当前发行版对应的安装文档完成安装。不要直接执行未审查的网络安装脚本：<https://docs.docker.com/engine/install/> 。安装后应能执行：

```bash
sudo docker version
sudo docker compose version
```

### 构建并测试

```bash
mkdir -p ~/cfst-docker/results
cd ~/cfst-docker
export PUID="$(id -u)"
export PGID="$(id -g)"
printf 'PUID=%s\nPGID=%s\n' "$PUID" "$PGID" > .env
chmod 600 .env

sudo docker compose build --pull
sudo docker compose run --rm cfst
```

完成后查看：

```bash
column -s, -t < results/result.csv
```

CSV 第二行是本轮结果中的首选候选。由于上游会从 IP 段随机抽取地址，每次结果可能不同；不能根据单次测试自动写入 DNS。

## 默认测试参数

默认使用 IPv4、50 个延迟线程、每个候选 4 次延迟测试、5 个下载测速候选、每个下载最多 5 秒、最大延迟 500ms、最大丢包率 10%。相比上游默认并发更保守，适合首次运行。

确认 VPS 网络与资源承受能力后，才可在 `docker-compose.yml` 中逐步提高 `-n`、`-dn` 或 `-dt`。不要直接使用 `-allip`，它会对 IPv4 段中的每个 IP 测试，负载和风险显著更高。

## IPv6 测试

确认 VPS 具备原生 IPv6 连通性后，使用下列命令覆盖默认参数：

```bash
docker compose run --rm cfst \
  -f /app/ipv6.txt \
  -o /results/result-ipv6.csv \
  -n 30 -t 4 -dn 5 -dt 5 -tl 500 -tlr 0.1
```

## HTTPing 模式

只有你拥有或获授权测试目标 HTTP 地址时才使用 `-httping`。上游说明指出该模式具有网络扫描性质，服务器上应降低并发，运营商或 CDN 可能临时限流。普通 TCPing + 下载测速已是默认模式。

## 安全与边界

- 镜像使用固定源码标签并在构建时验证 Git tag。
- 运行阶段使用只含静态二进制、CA 证书和 IP 列表的 `scratch` 镜像、非 root 用户、只读根文件系统、无 Linux capabilities 和资源限制。
- 结果目录是唯一可写挂载；不挂载 Docker Socket、主机目录、配置或密钥。
- 上游项目提示 Cloudflare CDN 禁止代理方式使用。本测试器只生成测量结果；是否以及如何使用 IP 必须另行确认服务条款与业务授权。

## 更新

在 `docker-compose.yml` 与 `Dockerfile` 同时把 `v2.3.5` 改为新的已审核 tag 后重新构建。不要改为 `latest`，以保证结果和构建可追溯。

## 通过 Docker Hub 部署

若不希望 VPS 编译镜像，可使用 GitHub Actions 发布镜像到自己的 Docker Hub 仓库，再由 VPS 直接拉取。完整步骤见 [DockerHub发布与VPS部署.md](DockerHub发布与VPS部署.md)。
