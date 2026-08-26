# syntax=docker/dockerfile:1.7
FROM golang:1.25.6
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
ENV GOTOOLCHAIN=local
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,id=go-mod-1.25.6,target=/cache/go-mod \
    GOMODCACHE=/cache/go-mod go mod download \
    && mkdir -p /go/pkg/mod \
    && cp -a /cache/go-mod/. /go/pkg/mod/
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
COPY web/package*.json ./web/
RUN cd web && npm install
COPY . .
RUN cd web && npm run build
RUN --mount=type=cache,id=go-build-1.25.6,target=/root/.cache/go-build go build ./...
CMD ["bash"]
