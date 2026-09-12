# CFST Web 控制台部署教程

Web 控制台是独立镜像，不替代现有的命令行镜像 `jieme520/cfst-tester:v2.3.5-1`。它在容器中执行同一份 CFST 程序，并通过网页管理测试参数、定时任务、结果和精简 IP 库。

## 工作方式

1. 候选库为空时，程序使用上游 `ip.txt` 进行一次全量网段收集。
2. 将本轮 TCP 可达、丢包符合上限的单个 IPv4 地址写入候选库。
3. 后续每次运行只复测候选库，节省扫描时间。
4. 只有在精简库复测中 TCP 不可达的 IP 才累计失败次数；达到网页设置的阈值（默认连续 3 次）才删除。
5. 下载速度为 `0.00` 不会触发删除，因为它可能由测速地址、HTTP 响应或临时网络问题造成。

`/app/ip.txt` 是镜像内只读的原始 IP 段，永远不会被删除。网页“清空候选库”仅删除 `/data/ip-library.txt` 的精简候选；下一次运行会重新全量收集。

## 发布 Web 镜像

1. 将本项目新增文件推送到 GitHub。
2. 在 GitHub 仓库的 **Actions** 中运行 **Publish CFST Web Console**。
3. `image_tag` 填写 `web-v0.1.0`。
4. 工作流成功后，Docker Hub 会出现 `jieme520/cfst-tester:web-v0.1.0`，其中同时包含 `amd64` 和 `arm64` 镜像。

该工作流继续使用原有 Docker Hub 用户变量与访问令牌；不需要新建 Docker Hub 仓库。

## VPS 与 1Panel 部署

先在 VPS 创建数据目录：

```bash
sudo mkdir -p /opt/dockerdata/cfst-web/data
sudo chown -R 65532:65532 /opt/dockerdata/cfst-web/data
sudo chmod 750 /opt/dockerdata/cfst-web/data
```

将 `docker-compose.web.yml` 上传到 VPS 的应用目录，命名为 `docker-compose.yml`，然后启动：

```bash
sudo docker compose pull
sudo docker compose up -d
sudo docker compose logs -f
```

Compose 使用 `network_mode: host`，但 Web 服务只监听 VPS 本机的 `127.0.0.1:8088`，不会直接暴露到公网。测速流量仍使用 VPS 本机的出站网络。

在 1Panel 的“网站”中建立反向代理时，目标填写：

```text
http://127.0.0.1:8088
```

反向代理公开到互联网前，务必在 `docker-compose.yml` 的 `environment:` 下取消注释并设置强密码：

```yaml
      CFST_WEB_USER: admin
      CFST_WEB_PASSWORD: 请替换为随机长密码
```

保存并执行 `sudo docker compose up -d` 重建容器。此登录保护覆盖页面和全部 API；不要把 Docker Socket 挂载进该容器。

## 页面操作

- 默认运行间隔为 6 小时。修改后从保存时重新计算下一次运行时间。
- 点击“立即测速”会立即运行一轮，不会与自动任务并行。
- “下载测速数量”默认 20，表示只对延迟排名靠前的 20 个 IP 进行下载测速；并非对全部候选进行带宽测试。
- 候选库和最新结果保存在 `/opt/dockerdata/cfst-web/data/`；每轮 CSV 位于 `data/results/`，程序保存最近 30 条任务摘要。

网页结果只反映运行该 VPS 的网络路径。若希望优化中国大陆用户访问体验，应在目标大陆网络中部署并测试，且根据服务条款和业务授权使用测试结果。
