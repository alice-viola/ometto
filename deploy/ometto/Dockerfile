# syntax=docker/dockerfile:1
#
# Ometto, the route planner, as one image: the Go service, the built page, the
# region map, the layers, the basemap and the two POI files. Queen and its
# Postgres are NOT here — they are the tenant's, reached over the network with
# a bearer token (see deploy/ometto/README.md).

# Cross-building: the two builder stages run on the machine doing the build
# (arm64 Mac), and only the final stage is the target platform. The page is
# static files, so it does not care; the binary is told which architecture to
# emit. Without this every npm and go step would run under emulation.

# --- the page -------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src
COPY web/app/package.json web/app/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/app/ ./
RUN npm run build

# --- the service ----------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/ ./third_party/
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Static, so the final image needs no libc of its own.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    GOFLAGS=-trimpath go build -ldflags "-s -w" -o /out/router ./cmd/router

# --- what ships -----------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget tzdata \
 && adduser -D -u 10001 -h /app ometto
WORKDIR /app

COPY --from=go   /out/router      /app/bin/router
COPY --from=web  /src/dist        /app/web/app/dist
# The data: the region map, the layers the page draws, the basemap, the names.
COPY web/public/taa.json          /app/web/public/taa.json
COPY web/public/region.geojson    /app/web/public/
COPY web/public/sat.geojson       /app/web/public/
COPY web/public/pois.geojson      /app/web/public/
COPY web/public/huts.geojson      /app/web/public/
COPY web/public/crags.geojson     /app/web/public/
COPY web/public/lifts.geojson     /app/web/public/
COPY web/tiles/                   /app/web/tiles/
COPY data/osm-taa/places.json     /app/data/osm-taa/places.json
COPY data/osm-taa/pois.json       /app/data/osm-taa/pois.json
# The stamp the page shows in its About panel: where the data came from and
# when. Written by tools/build-info.py, so run the pipeline before building.
COPY data/build-info.json         /app/data/build-info.json

USER ometto
EXPOSE 8100

# Everything is configuration; nothing here is a secret.
ENV QUEEN_URL=http://queen:6633 \
    QUEEN_PREFIX=ometto. \
    TILES_DIR=/app/web/tiles \
    BUILD_INFO=/app/data/build-info.json \
    LOG_LEVEL=info

HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8100/api/health | grep -q '"ok":true' || exit 1

ENTRYPOINT ["/app/bin/router"]
CMD ["-city", "/app/web/public/taa.json", \
     "-places", "/app/data/osm-taa/places.json", \
     "-pois", "/app/data/osm-taa/pois.json", \
     "-layers", "/app/web/public", \
     "-static", "/app/web/app/dist", \
     "-addr", ":8100", \
     "-region", "taa", \
     "-workers", "4"]
