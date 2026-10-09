#!/bin/sh
# metaxisdata container entrypoint: maps environment variables onto server
# flags and execs the binary. It is PID 1 in the container, so it must hand the
# process over (`exec`) rather than daemonize; no signal is trapped here, and
# the server's own SIGTERM handler is what makes `docker stop` a graceful
# shutdown.
#
# The image is the server only — it has no subcommands and no setup step — so
# this script is deliberately thin compared to a multi-role deployment's.
#
# Environment (all optional except PG_URL, which the server itself reads):
#   PG_URL                              PostgreSQL connection URL (required)
#   METAXISDATA_ENCRYPTION_KEY          wraps the per-deployment data key
#   METAXISDATA_ENCRYPTION_KEY_PREVIOUS
#   METAXISDATA_PORT                    listening port (default 8083)
#   METAXISDATA_DEBUG=true              debug-level logging
#   METAXISDATA_JSON_LOGGING=true       JSON logs instead of text
#   METAXISDATA_CORS_ALLOW_ORIGINS      comma-separated browser origins
#   METAXISDATA_TRUSTED_PROXIES         comma-separated IPs/CIDRs whose
#                                       X-Forwarded-For is believed for audit
set -eu

# Arguments the caller appends (`docker run … --debug`) are passed through and
# win over the values derived below.
# Every derived flag is prepended, never appended: an explicit argument from
# `docker run …` / compose `command:` ends up last and wins, because the server
# takes the last value of a repeated flag.
set -- --port "${METAXISDATA_PORT:-8083}" "$@"

if [ "${METAXISDATA_DEBUG:-false}" = "true" ]; then
	set -- --debug "$@"
fi
if [ "${METAXISDATA_JSON_LOGGING:-false}" = "true" ]; then
	set -- --enable-json-logging "$@"
fi
if [ -n "${METAXISDATA_CORS_ALLOW_ORIGINS:-}" ]; then
	set -- --cors-allow-origins "${METAXISDATA_CORS_ALLOW_ORIGINS}" "$@"
fi
if [ -n "${METAXISDATA_TRUSTED_PROXIES:-}" ]; then
	set -- --trusted-proxies "${METAXISDATA_TRUSTED_PROXIES}" "$@"
fi

exec metaxisdata "$@"
