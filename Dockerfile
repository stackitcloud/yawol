# syntax=docker/dockerfile:1.6-labs
ARG GO_VERSION=1.21

# get modules, if they don't change the cache can be used for faster builds
FROM golang:${GO_VERSION} AS base
ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /src
COPY go.* ./
RUN go mod download

# yawol-cloud-controller
FROM base AS yawol-cloud-controller-build
COPY api/ api/
COPY cmd/ cmd/
COPY controllers/ controllers/
COPY internal/ internal/
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/yawol-cloud-controller ./cmd/yawol-cloud-controller/main.go

FROM gcr.io/distroless/static:nonroot AS yawol-cloud-controller
COPY --from=yawol-cloud-controller-build /app/yawol-cloud-controller /bin/yawol-cloud-controller
USER 65532:65532
ENTRYPOINT ["/bin/yawol-cloud-controller"]

# yawol-controller
FROM base AS yawol-controller-build
COPY api/ api/
COPY cmd/ cmd/
COPY controllers/ controllers/
COPY internal/ internal/
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/yawol-controller ./cmd/yawol-controller/main.go

# Import the binary from build stage
FROM gcr.io/distroless/static:nonroot AS yawol-controller
COPY --from=yawol-controller-build /app/yawol-controller /bin/yawol-controller
USER 65532:65532
ENTRYPOINT ["/bin/yawol-controller"]
