# syntax=docker/dockerfile:1.3-labs
ARG GO_VERSION=1.17

# get modules, if they don't change the cache can be used for faster builds
FROM golang:${GO_VERSION} AS base
ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /src
COPY go.* .
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# yawol-cloud-controller
FROM base AS yawol-cloud-controller-build
# temp mount all files instead of loading into image with COPY
# temp mount module cache
# temp mount go build cache
RUN --mount=target=. \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/yawol-cloud-controller ./cmd/yawol-cloud-controller/main.go

FROM gcr.io/distroless/static:nonroot AS yawol-cloud-controller
COPY --from=yawol-cloud-controller-build /app/yawol-cloud-controller /bin/yawol-cloud-controller
USER 65532:65532
ENTRYPOINT ["/bin/yawol-cloud-controller"]

# yawol-cloud-controller
FROM base AS yawol-controller-build
# temp mount all files instead of loading into image with COPY
# temp mount module cache
# temp mount go build cache
RUN --mount=target=. \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/yawol-controller ./cmd/yawol-controller/main.go

# Import the binary from build stage
FROM gcr.io/distroless/static:nonroot AS yawol-controller
COPY --from=yawol-controller-build /app/yawol-controller /bin/yawol-controller
USER 65532:65532
ENTRYPOINT ["/bin/yawol-cloud-controller"]
