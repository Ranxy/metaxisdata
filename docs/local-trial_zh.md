> **语言 / Language:** [English](local-trial.md) | [中文](local-trial_zh.md)

# 用 compose 做本地试用

[docker-compose.yml](../docker-compose.yml) 会把 PostgreSQL 和服务端一起启动，所以在本机看一眼构建出来的镜像，`make docker-up` 就够了。它有意不是一个生产拓扑：部署会自带 PostgreSQL，并在反向代理上终止 TLS——那部分见 [deploy_zh.md](deploy_zh.md)。

```bash
make docker-up                 # 构建，然后启动 PostgreSQL + 服务端，访问 http://localhost:8083
docker compose logs -f server
make docker-down               # 停止两者，保留数据卷——详见下文

docker compose up -d           # 同上，只是不用 make
docker compose up -d --build   # ……并先重新构建镜像
```

打开 <http://localhost:8083>，创建第一个账号——它会成为工作区管理员；在管理员于 *设置* → *通用设置* 中开启「禁止自助注册」之前，注册通道一直开放。

## compose 文件是什么

`docker-compose.yml` 发布 8083 端口，把 PostgreSQL 放在一个命名卷上，并硬编码了一个开发用密码。里面没有任何东西是准备带到生产环境去的。

`make docker-down` 会停止两者并保留该数据卷。想连数据一起删除，请执行 `docker compose down -v`——给 `make` 传 `-v` 是没用的，GNU make 会把它当成自己的参数，目标根本不会执行。

## `make docker-up` 做了什么

它总是从当前检出目录重新构建，所以你运行的镜像和你手上的代码一致。`VERSION` 既是镜像标签也是二进制对外报告的版本，两者不可能不一致：`VERSION=v1.2.3 make docker-up` 运行的镜像名是 `metaxisdata/metaxisdata:v1.2.3`。

## 改用已发布的镜像

先拉取镜像，然后**不要**重新构建地启动整个栈——`make docker-up` 总会带上 `--build`，那会用它构建出来的镜像覆盖这个名字，而不是使用拉取到的镜像：

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3
METAXISDATA_IMAGE=ghcr.io/ranxy/metaxisdata VERSION=v1.2.3 docker compose up -d
```

## `:dev` 标签的注意事项

`:dev` 同时是 `make docker-build`（release profile）和 `make docker-build-dev`（dev profile）使用的标签，所以一次 dev 构建之后直接 `docker compose up` 会启动 dev profile。`make docker-up` 总会重新构建，这也是它被写成文档路径的原因。
