> **Language / 语言:** [English](local-trial.md) | [中文](local-trial_zh.md)

# Local trial with compose

[docker-compose.yml](../docker-compose.yml) starts PostgreSQL and the server together, so `make docker-up` is enough to look at a built image on your own machine. It is deliberately not a production topology: a deployment brings its own PostgreSQL and terminates TLS in a reverse proxy — that part is [deploy.md](deploy.md).

```bash
make docker-up                 # builds, then PostgreSQL + the server on http://localhost:8083
docker compose logs -f server
make docker-down               # stop both; keeps the data volume — see below

docker compose up -d           # the same, without make
docker compose up -d --build   # …and rebuild the image first
```

Open <http://localhost:8083> and create the first account — it becomes the workspace administrator, and signup stays open until an administrator turns on *Disallow self-service signup* in *Settings* → *General*.

## What the compose file is

`docker-compose.yml` publishes port 8083, keeps PostgreSQL on a named volume, and hardcodes a development password. Nothing in it is meant to survive contact with production.

`make docker-down` stops both and keeps that volume. To drop the data with it, run `docker compose down -v` — passing `-v` to `make` does not do this, because GNU make takes it as its own flag and never runs the target.

## What `make docker-up` does

It always rebuilds from this checkout, so the image you run matches the code you
have. `VERSION` is both the image tag and the version the binary reports, so the
two cannot disagree: `VERSION=v1.2.3 make docker-up` runs
`metaxisdata/metaxisdata:v1.2.3`.

## Running a published image instead

Pull the image, then bring the stack up **without** rebuilding — `make docker-up` always passes `--build`, which would build over the tag instead of using it:

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3
METAXISDATA_IMAGE=ghcr.io/ranxy/metaxisdata VERSION=v1.2.3 docker compose up -d
```

## The `:dev` caveat

`:dev` is the tag of both `make docker-build` (release profile) and `make docker-build-dev` (dev profile), so a bare `docker compose up` after a dev build would start the dev profile. `make docker-up` always rebuilds, which is why it is the documented path.
