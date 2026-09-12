# Docker Hub 发布与 VPS 部署

## 一次性准备

1. 在 Docker Hub 创建仓库，例如 `<你的 Docker ID>/cfst-tester`。
2. 在 Docker Hub 的 Account settings -> Personal access tokens 创建一个仅用于发布的 PAT，权限选 `Read & Write`、设置合理过期时间。不要使用账号密码，不要把 Token 写入任何文件。
3. 在 GitHub 创建一个空仓库，例如 `cfst-docker`。将本目录的**内容**上传到仓库根目录，必须包含 `.github/workflows/publish-dockerhub.yml`、`Dockerfile` 和 `docker-compose.hub.yml`。
4. 在 GitHub 仓库 Settings -> Secrets and variables -> Actions：
   - 创建 Repository variable：`DOCKERHUB_USERNAME`，值为你的 Docker ID。
   - 创建 Repository secret：`DOCKERHUB_TOKEN`，值为第 2 步的 PAT。

## 发布镜像

进入 GitHub 仓库 Actions -> `Publish to Docker Hub` -> Run workflow。输入不可变标签，例如 `v2.3.5-1`。

工作流会在 GitHub 托管 runner 上构建 Linux `amd64` 与 `arm64` 镜像，并发布为：

```
<你的 Docker ID>/cfst-tester:v2.3.5-1
```

发布完成后，在 Docker Hub 的仓库 Tags 页面确认标签存在。不要部署 `latest` 或会被覆盖的标签。

### 发布失败：`buildx failed` / `attestations`

本项目的工作流已关闭 `provenance` 与 `sbom` attestation，避免部分 Docker Hub 仓库拒绝 OCI attestation manifest。若 GitHub 仓库中仍是旧工作流，请将 `.github/workflows/publish-dockerhub.yml` 同步为本目录最新版本后再执行 `Re-run jobs`。登录步骤成功而 Build and push 失败时，不要重新生成 Docker Hub Token。

## 在 VPS 部署

在 VPS 中只需创建一个运行目录，不需要克隆源码或构建镜像：

```bash
mkdir -p ~/cfst-run/results
cd ~/cfst-run
```

将 `docker-compose.hub.yml` 上传到该目录，然后创建 `.env`：

```bash
cat > .env <<'EOF'
PUID=1000
PGID=1000
CFST_IMAGE=YOUR_DOCKER_ID/cfst-tester:v2.3.5-1
EOF
```

将 `PUID` 和 `PGID` 改成执行命令用户的 `id -u` 与 `id -g` 输出，`CFST_IMAGE` 改成实际镜像地址。随后执行：

```bash
sudo docker compose -f docker-compose.hub.yml pull
sudo docker compose -f docker-compose.hub.yml run --rm cfst
column -s, -t < results/result.csv
```

## 私有仓库

若 Docker Hub 仓库为私有，在 VPS 先执行 `sudo docker login`，用户名为 Docker ID，密码粘贴 PAT；登录成功后再运行 `pull`。公开镜像不需要 VPS 登录。

## 更新与回退

每次修改 Dockerfile 后发布新标签，例如 `v2.3.5-2`，在 VPS 只修改 `.env` 内的 `CFST_IMAGE` 并重新 `pull`。需要回退时改回旧标签。保留至少一个已验证可用的旧标签。
