VERSION 0.6
FROM golang:1.18
ARG DOCKER_REPO=reg.infra.ske.eu01.stackit.cloud/ske/
ARG BINPATH=/usr/local/bin/
ARG GOCACHE=/go-cache

local-setup:
    LOCALLY
    RUN git config --local core.hooksPath .githooks/

deps:
    WORKDIR /src
    ENV GO111MODULE=on
    ENV CGO_ENABLED=0
    COPY go.mod go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum

build:
    FROM +deps
    COPY --dir controllers/ internal/ cmd/ api/ .
    ARG CONTROLLER
    ARG GOOS=linux
    ARG GOARCH=amd64
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o controller ./cmd/$CONTROLLER/main.go
    SAVE ARTIFACT controller

local:
    ARG USEROS
    ARG USERARCH
    ARG CONTROLLER
    COPY (+build/controller --CONTROLLER=$CONTROLLER --GOOS=$USEROS --GOARCH=$USERARCH) /controller
    SAVE ARTIFACT /controller AS LOCAL out/$CONTROLLER

build-local:
  BUILD +local --CONTROLLER=yawol-controller
  BUILD +local --CONTROLLER=yawol-cloud-controller

build-test:
    FROM +deps
    COPY --dir controllers/ internal/ cmd/ api/ .
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o /dev/null ./...

set-version:
    FROM alpine/git
    COPY .git .git
    RUN git describe --tags --always > VERSION
    SAVE ARTIFACT VERSION

ci:
    FROM busybox
    COPY +set-version/VERSION .
    BUILD +docker --CONTROLLER=yawol-controller --DOCKER_TAG=$(cat VERSION)
    BUILD +docker --CONTROLLER=yawol-cloud-controller --DOCKER_TAG=$(cat VERSION)

docker:
    ARG TARGETPLATFORM
    ARG TARGETOS
    ARG TARGETARCH
    ARG DOCKER_TAG
    ARG CONTROLLER
    FROM --platform=$TARGETPLATFORM \
        gcr.io/distroless/static:nonroot
    COPY --platform=$USERPLATFORM \
        (+build/controller --CONTROLLER=$CONTROLLER --GOOS=$TARGETOS --GOARCH=$TARGETARCH) /controller
    BUILD +set-version
    USER 65532:65532
    ENTRYPOINT ["/controller"]
    SAVE IMAGE --push $DOCKER_REPO$CONTROLLER:$DOCKER_TAG

generate:
    FROM +deps
    COPY +controller-gen/bin/controller-gen $BINPATH
    COPY --dir api/ .
    RUN controller-gen object paths="./..."
    SAVE ARTIFACT ./api/* AS LOCAL api/

lint:
    ARG GOLANGCI_LINT_CACHE=/golangci-cache
    FROM +deps
    COPY +golangci-lint/golangci-lint $BINPATH
    COPY --dir controllers/ internal/ cmd/ api/ .golangci.yml .
    RUN --mount=type=cache,target=$GOCACHE \
        --mount=type=cache,target=$GOLANGCI_LINT_CACHE \
        golangci-lint run -v ./...

test:
    FROM +deps
    ARG KUBERNETES_VERSION=1.23.x
    COPY +gotools/bin/setup-envtest $BINPATH
    COPY +envoy/envoy $BINPATH
    # install envtest in its own layer
    RUN setup-envtest use $KUBERNETES_VERSION
    # charts folder is needed for the CRDs
    # image folder is needed for envoy
    COPY --dir image/ internal/ cmd/ controllers/ api/ charts/ .
    ARG GO_TEST="go test"
    RUN --mount=type=cache,target=$GOCACHE \
        if [ ! "$(ls -A $GOCACHE)" ]; then echo "WAITING FOR GO TEST TO BUILD TESTING BIN"; fi && \
        eval `setup-envtest use -p env $KUBERNETES_VERSION` && \
        eval "$GO_TEST ./..."

test-output:
    FROM +test --GO_TEST="go test -count 1 -coverprofile=cover.out"
    SAVE ARTIFACT cover.out

coverage:
    FROM +deps
    COPY --dir internal/ api/ cmd/ controllers/ .
    COPY +test-output/cover.out .
    RUN go tool cover -func=cover.out

coverage-html:
    LOCALLY
    COPY +test-output/cover.out out/cover.out
    RUN go tool cover -html=out/cover.out

snyk-go:
    FROM +deps
    COPY +snyk/snyk $BINPATH
    COPY --dir controllers/ internal/ cmd/ charts/ api/ .
    COPY .snyk .
    RUN --secret SNYK_TOKEN snyk test

snyk-helm:
    FROM +deps
    COPY +snyk/snyk $BINPATH
    COPY --dir +helm2kube/result .
    COPY .snyk .
    RUN --secret SNYK_TOKEN \
        snyk iac test \
        --policy-path=.snyk \ # I don't know why the CLI won't pick this up by default...
        --severity-threshold=high \  # TODO remove this line if you want to fix a lot of issues in the helm charts
        result

# todo: semgrep
# semgrep:

all:
    BUILD +generate
    BUILD +snyk-go
    BUILD +snyk-helm
    BUILD +lint
    BUILD +coverage
    BUILD +test
    BUILD +ci

###########
# helper
###########

golangci-lint:
    FROM golangci/golangci-lint:v1.48.0
    SAVE ARTIFACT /usr/bin/golangci-lint

envoy:
    FROM envoyproxy/envoy:v1.21.1
    SAVE ARTIFACT /usr/local/bin/envoy

snyk:
    FROM snyk/snyk:alpine
    SAVE ARTIFACT /usr/local/bin/snyk

helm:
    FROM alpine/helm:3.8.1
    SAVE ARTIFACT /usr/bin/helm

bash:
    FROM bash
    SAVE ARTIFACT /usr/local/bin

helm2kube:
    COPY +helm/helm $BINPATH
    COPY --dir ./charts/yawol-controller .
    RUN mkdir result
    RUN helm template ./yawol-controller >> result/yawol-controller.yaml
    SAVE ARTIFACT result

gotools:
    FROM +deps
    # tool versions tracked in go.mod through tools.go
    RUN go install \
        sigs.k8s.io/controller-runtime/tools/setup-envtest
    SAVE ARTIFACT /go/bin

controller-gen:
    FROM +deps
    # tool versions tracked in go.mod through tools.go
    RUN go install \
        sigs.k8s.io/controller-tools/cmd/controller-gen
    SAVE ARTIFACT /go/bin
