# jieme520/cfst-tester VPS 部署教程

已验证镜像：

- 标签：`jieme520/cfst-tester:v2.3.5-1`
- OCI 多架构 manifest digest：`sha256:cf24961a3e3dc08b0887f9e2758059ca0b03f3ea88fac4b95fefa01c2973bdb6`
- 平台：Linux `amd64`、Linux `arm64`

建议部署时使用 digest，而不是仅使用 tag。Docker 会从这个多架构 manifest 中自动选取与 VPS CPU 匹配的镜像。

## 前提

VPS 为 Linux，且已经安装 Docker Engine。确认：

```bash
sudo docker version
uname -m
```

`x86_64` 会自动拉取 AMD64；`aarch64` 或 `arm64` 会自动拉取 ARM64。

## 单次测速（推荐先执行）

```bash
sudo mkdir -p /opt/dockerdata/cfst/results
sudo chown -R 65532:65532 /opt/dockerdata/cfst/results
cd /opt/dockerdata/cfst

sudo docker pull jieme520/cfst-tester@sha256:cf24961a3e3dc08b0887f9e2758059ca0b03f3ea88fac4b95fefa01c2973bdb6

sudo docker run --rm \
  --network host \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  --user "$(id -u):$(id -g)" \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --pids-limit 128 \
  --memory 256m \
  -v /opt/dockerdata/cfst/results:/results:rw \
  jieme520/cfst-tester@sha256:cf24961a3e3dc08b0887f9e2758059ca0b03f3ea88fac4b95fefa01c2973bdb6 \
  -f /app/ip.txt \
  -o /results/result.csv \
  -n 50 -t 4 -dn 5 -dt 5 -tl 500 -tlr 0.1
```

结果：

```bash
sed -n '1,11p' /opt/dockerdata/cfst/results/result.csv
```

第二行是本轮首选候选。请至少多次测试，并验证真实 HTTPS 服务后再将结果用于任何 DNS 变更。

## IPv6 测速

仅在 `ip -6 addr` 显示公网 IPv6 地址，且 VPS 能正常访问 IPv6 网络时使用：

```bash
sudo docker run --rm \
  --network host --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  --user "$(id -u):$(id -g)" \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --pids-limit 128 --memory 256m \
  -v /opt/dockerdata/cfst/results:/results:rw \
  jieme520/cfst-tester@sha256:cf24961a3e3dc08b0887f9e2758059ca0b03f3ea88fac4b95fefa01c2973bdb6 \
  -f /app/ipv6.txt \
  -o /results/result-ipv6.csv \
  -n 30 -t 4 -dn 5 -dt 5 -tl 500 -tlr 0.1
```

## 建议与回退

- 首次使用保持低并发；不要使用 `-allip`。
- 先手动运行并保留 CSV，暂不设置 cron 自动化。
- Docker Hub 当前未启用 immutable tags；继续发布新版本时使用新 tag 和 manifest digest，勿覆盖已部署版本。
- 若需停止，无后台服务需要清理；容器以 `--rm` 模式执行结束后自动删除，结果文件保留在 `/opt/dockerdata/cfst/results/`。
