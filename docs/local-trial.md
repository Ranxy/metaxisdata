> **Language / 语言:** [English](local-trial.md) | [中文](local-trial_zh.md)

# Local trial with compose

[docker-compose.yml](../docker-compose.yml) starts PostgreSQL and the server
together, so `make docker-up` is enough to try a built image on your own
machine. It is not a production topology: a deployment brings its own
PostgreSQL and terminates TLS in a reverse proxy — that is
[deploy.md](deploy.md).

```bash
make docker-up                 # build, then PostgreSQL + the server on http://localhost:8083
docker compose logs -f server
make docker-down               # stop both; the data volume is kept

docker compose up -d           # the same, without make
docker compose up -d --build   # …and rebuild the image first
```

Open <http://localhost:8083> and create the first account — it becomes the
workspace administrator. Signup stays open until an administrator turns on
*Disallow self-service signup* in *Settings* → *General*.

## What the stack looks like

`docker-compose.yml` publishes port 8083, keeps PostgreSQL data on a named
volume, and hardcodes a development password. None of that belongs in
production.

`make docker-down` stops both and keeps the data volume. To drop the data as
well, run `docker compose down -v` — appending `-v` to `make` does not do
this: GNU make takes it as its own flag, and the target never runs.

## What `make docker-up` does

It always rebuilds from this checkout, so the image you run matches the code
you have. `VERSION` is both the image tag and the version the binary reports,
so the two cannot disagree: `VERSION=v1.2.3 make docker-up` runs
`metaxisdata/metaxisdata:v1.2.3`.

## Running a published image instead

Pull the image, then start the stack without a rebuild — `make docker-up`
always passes `--build`, which would build over the tag instead of using it:

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3
METAXISDATA_IMAGE=ghcr.io/ranxy/metaxisdata VERSION=v1.2.3 docker compose up -d
```

## The `:dev` tag caveat

`:dev` is the tag of both `make docker-build` (release profile) and
`make docker-build-dev` (dev profile). After a dev-profile build, a bare
`docker compose up` starts the dev-profile image. `make docker-up` always
rebuilds, which is why it is the documented path.