# Compose 部署代码

使用文件：

- `docker-compose.jieme520.yml`

## VPS 操作

```bash
sudo mkdir -p /opt/dockerdata/cfst/results
sudo chown -R 65532:65532 /opt/dockerdata/cfst/results
cd /opt/dockerdata/cfst
```

将 `docker-compose.jieme520.yml` 上传到该目录并命名为 `docker-compose.yml`。本文件不依赖 `.env`，可直接在 Dockge 或 Docker Compose 使用。

拉取并单次运行：

```bash
sudo docker compose pull
sudo docker compose run --rm cfst
sed -n '1,11p' /opt/dockerdata/cfst/results/result.csv
```

该 Compose 使用 `jieme520/cfst-tester:v2.3.5-1` 标签。Docker 会按 VPS 的 `amd64` 或 `arm64` CPU 自动选择镜像。若发布后需要升级，请发布新版本并更新 `image:` 行；建议不要覆盖已部署的标签。

镜像默认以 UID/GID `65532` 的非 root 用户运行，因此结果目录必须由 `65532:65532` 拥有，否则输出 CSV 会因权限不足失败。

## Dockge 权限故障

若日志显示以下错误，测速已经执行完毕，只是无法将内存中的结果写入宿主机目录：

```text
open /results/result.csv: permission denied
```

在 Dockge 的 Compose 编辑器中删除旧版配置中的整行 `user: ${PUID...}:${PGID...}`，保存后停止并重新启动容器。然后在 VPS 终端执行：

```bash
sudo mkdir -p /opt/dockerdata/cfst/results
sudo chown -R 65532:65532 /opt/dockerdata/cfst/results
sudo chmod 750 /opt/dockerdata/cfst/results
```

重新运行后，结果会写入 `/opt/dockerdata/cfst/results/result.csv`。本次失败运行的结果不会自动保留，需要重新测速一次。
