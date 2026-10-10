> **语言 / Language:** [English](local-trial.md) | [中文](local-trial_zh.md)

# 用 compose 做本地试用

[docker-compose.yml](../docker-compose.yml) 会把 PostgreSQL 和服务端一起启动，因此在你的机器上试用一个构建出来的镜像，`make docker-up` 就足够了。它不是生产拓扑：正式部署会自带 PostgreSQL，并在反向代理上终止 TLS——那部分见 [deploy_zh.md](deploy_zh.md)。

```bash
make docker-up                 # 构建，然后启动 PostgreSQL + 服务端，访问 http://localhost:8083
docker compose logs -f server
make docker-down               # 停止两者；数据卷会保留

docker compose up -d           # 同上，不经过 make
docker compose up -d --build   # ……并先重新构建镜像
```

打开 <http://localhost:8083>，创建第一个账号——它会成为工作区管理员。在管理员于 *设置* → *通用设置* 中开启「禁止自助注册」之前，注册通道一直开放。

## compose 栈是什么样子

`docker-compose.yml` 对外发布 8083 端口，把 PostgreSQL 数据放在一个命名卷上，并硬编码了一个开发用密码。这几样都不该带进生产环境。

`make docker-down` 会停止两者并保留数据卷。想连数据一起删除，请执行 `docker compose down -v`——给 `make` 附加 `-v` 做不到这一点：GNU make 会把它当成自己的参数，目标根本不会执行。

## `make docker-up` 做了什么

它总是从当前检出目录重新构建，所以你运行的镜像和你手上的代码一致。`VERSION` 既是镜像标签也是二进制对外报告的版本，两者不可能不一致：`VERSION=v0.1.0 make docker-up` 运行的镜像名是 `metaxisdata/metaxisdata:v0.1.0`。

## 改用已发布的镜像

先拉取镜像，然后不带重新构建地启动整个栈——`make docker-up` 总会带上 `--build`，那会用构建产物覆盖这个标签，而不是使用拉取到的镜像：

```bash
docker pull ghcr.io/ranxy/metaxisdata:v0.1.0
METAXISDATA_IMAGE=ghcr.io/ranxy/metaxisdata VERSION=v0.1.0 docker compose up -d
```

## `:dev` 标签的注意事项

`:dev` 同时是 `make docker-build`（release profile）和 `make docker-build-dev`（dev profile）使用的标签。dev profile 构建之后，直接 `docker compose up` 启动的是 dev profile 的镜像。`make docker-up` 总会重新构建，这也是文档推荐走它的原因。