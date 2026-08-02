# syntax=docker/dockerfile:1

# builder
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Зависимости копируются отдельно от исходников: слой с go mod download переиспользуется, пока go.mod/go.sum не изменились.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# CGO_ENABLED=0 даёт статический бинарь, необходимый для distroless static.
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/migrator ./cmd/migrator

# runtime
# distroless static: только бинарь, сертификаты и часовые пояса
FROM gcr.io/distroless/static:nonroot AS runtime

WORKDIR /
COPY --from=builder /out/api /api
COPY --from=builder /out/migrator /migrator

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/api"]
