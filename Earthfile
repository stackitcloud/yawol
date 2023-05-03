VERSION 0.6
FROM golang:1.19
ARG DOCKER_REPO=ghcr.io/stackitcloud/yawol/
ARG BINPATH=/usr/local/bin/
ARG GOCACHE=/go-cache

ARG ENVOY_VERSION=v1.24.0
ARG PROMTAIL_VERSION=2.7.0
ARG HELM_VERSION=3.8.1
ARG GOLANGCI_LINT_VERSION=v1.50.1
ARG PACKER_VERSION=1.8
ARG TERRAFORM_VERSION=1.3.7

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

helm-docs:
    FROM +deps
    COPY --dir charts/ .
    ENV GO111MODULE=on
    RUN GO111MODULE=on go install github.com/norwoodj/helm-docs/cmd/helm-docs@v1.11.0
    RUN helm-docs charts/yawol-controller/
    SAVE ARTIFACT charts/yawol-controller/README.md AS LOCAL charts/yawol-controller/README.md

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
  BUILD +local --CONTROLLER=yawollet

build-test:
    FROM +deps
    COPY --dir controllers/ internal/ cmd/ api/ .
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o /dev/null ./...

get-envoy-local:
    FROM +envoy
    COPY +get-envoy/envoy /envoy
    SAVE ARTIFACT /envoy AS LOCAL out/envoy/envoy

get-envoy-libs-local:
    FROM +envoy
    COPY +get-envoy/envoylibs /envoylibs
    SAVE ARTIFACT /envoylibs AS LOCAL out/envoy/lib

get-promtail-local:
    FROM +promtail
    COPY +promtail/promtail /promtail
    SAVE ARTIFACT /promtail AS LOCAL out/promtail

get-envoy:
    FROM +envoy
    SAVE ARTIFACT /usr/local/bin/envoy
    SAVE ARTIFACT /lib/x86_64-linux-gnu/ld-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libm-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libm.* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/librt-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/librt.* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libdl-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libdl.* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libpthread-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libpthread.* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libc-* /envoylibs/
    SAVE ARTIFACT /lib/x86_64-linux-gnu/libc.* /envoylibs/

validate-yawollet-image:
    BUILD +local --CONTROLLER=yawollet
    FROM +packer
    COPY image image
	RUN packer fmt -check -diff image/alpine-yawol.pkr.hcl
    RUN packer validate -syntax-only \
      -var 'os_project_id=UNSET' \
      -var 'source_image=UNSET' \
      -var 'image_tags=[]' \
      -var 'image_version=UNSET' \
      -var 'network_id=UNSET' \
      -var 'floating_network_id=UNSET' \
      -var 'security_group_id=UNSET' \
      -var 'machine_flavor=UNSET' \
      -var 'volume_type=UNSET' \
      -var 'image_visibility=private' \
      -var 'multiqueue_enabled=false' \
      image/alpine-yawol.pkr.hcl

build-yawollet-image:
    FROM +packer

    ARG USEROS
    ARG USERARCH

    ARG MACHINE_FLAVOR=c1.2
    ARG VOLUME_TYPE=storage_premium_perf6
    ARG MULTIQUEUE_ENABLED=False

    ARG --required IMAGE_VISIBILITY
    ARG --required OS_SOURCE_IMAGE
    ARG --required OS_NETWORK_ID
    ARG --required OS_FLOATING_NETWORK_ID
    ARG --required OS_SECURITY_GROUP_ID

    ARG --required OS_AUTH_URL
    ARG --required OS_PROJECT_ID
    ARG --required OS_PROJECT_NAME
    ARG --required OS_USER_DOMAIN_NAME
    ARG --required OS_PASSWORD
    ARG --required OS_USERNAME
    ARG --required OS_REGION_NAME

    COPY +promtail/promtail out/promtail
    COPY +get-envoy/envoy out/envoy/envoy
    COPY +get-envoy/envoylibs out/envoy/lib
    COPY (+build/controller --CONTROLLER=yawollet --GOOS=$USEROS --GOARCH=$USERARCH) out/yawollet
    COPY +set-version/VERSION .

    COPY image image

    RUN packer build \
      -var "source_image=$OS_SOURCE_IMAGE" \
      -var "image_version=$(cat VERSION)" \
      -var "image_tags=[\"$(cat VERSION)\", \"yawol\"]" \
      -var "os_project_id=$OS_PROJECT_ID" \
      -var "network_id=$OS_NETWORK_ID" \
      -var "floating_network_id=$OS_FLOATING_NETWORK_ID" \
      -var "security_group_id=$OS_SECURITY_GROUP_ID" \
      -var "machine_flavor=$MACHINE_FLAVOR" \
      -var "volume_type=$VOLUME_TYPE" \
      -var "image_visibility=$IMAGE_VISIBILITY" \
      -var "multiqueue_enabled=$MULTIQUEUE_ENABLED" \
      image/alpine-yawol.pkr.hcl

build-packer-environment:
    FROM +terraform

    ARG FLOATING_NETWORK_NAME=floating-net

    ARG --required OS_AUTH_URL
    ARG --required OS_PROJECT_ID
    ARG --required OS_PROJECT_NAME
    ARG --required OS_USER_DOMAIN_NAME
    ARG --required OS_PASSWORD
    ARG --required OS_USERNAME
    ARG --required OS_REGION_NAME

    COPY --dir hack/packer-infrastructure .

    WORKDIR /packer-infrastructure

    RUN terraform init
    RUN terraform apply \
      -var "floating_ip_network_name=$FLOATING_NETWORK_NAME" \
      -auto-approve

    SAVE ARTIFACT /packer-infrastructure/terraform.tfstate AS LOCAL hack/packer-infrastructure/terraform.tfstate

destroy-packer-environment:
    FROM +terraform

    ARG FLOATING_NETWORK_NAME=floating-net

    ARG --required OS_AUTH_URL
    ARG --required OS_PROJECT_ID
    ARG --required OS_PROJECT_NAME
    ARG --required OS_USER_DOMAIN_NAME
    ARG --required OS_PASSWORD
    ARG --required OS_USERNAME
    ARG --required OS_REGION_NAME

    COPY --dir hack/packer-infrastructure .

    WORKDIR /packer-infrastructure

    RUN terraform init
    RUN terraform destroy \
      -var "floating_ip_network_name=$FLOATING_NETWORK_NAME" \
      -auto-approve

    SAVE ARTIFACT /packer-infrastructure/terraform.tfstate* AS LOCAL hack/packer-infrastructure/


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
    RUN controller-gen crd rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config="crds"
    SAVE ARTIFACT ./api/* AS LOCAL api/
    SAVE ARTIFACT ./crds/* AS LOCAL charts/yawol-controller/crds/

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
    ARG KUBERNETES_VERSION=1.24.x
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
    COPY +snyk-linux/snyk $BINPATH
    COPY --dir controllers/ internal/ cmd/ charts/ api/ .
    COPY .snyk .
    RUN --secret SNYK_TOKEN snyk test

snyk-helm:
    FROM alpine/helm:$HELM_VERSION
    COPY +snyk-alpine/snyk $BINPATH
    COPY --dir +helm2kube/result .
    COPY .snyk .
    RUN --secret SNYK_TOKEN \
        snyk iac test \
        --policy-path=.snyk \ # I don't know why the CLI won't pick this up by default...
        --severity-threshold=high \  # TODO remove this line if you want to fix a lot of issues in the helm charts
        result

# todo: semgrep
# semgrep:

snyk:
    BUILD +generate
    BUILD +snyk-go
    BUILD +snyk-helm

all-except-snyk:
    BUILD +generate
    BUILD +lint
    BUILD +coverage
    BUILD +test
    BUILD +ci

all:
    BUILD +snyk
    BUILD +all-except-snyk

###########
# helper
###########

golangci-lint:
    FROM golangci/golangci-lint:$GOLANGCI_LINT_VERSION
    SAVE ARTIFACT /usr/bin/golangci-lint

packer:
    FROM hashicorp/packer:$PACKER_VERSION
    RUN apk add ansible
    RUN apk add openssh-client

terraform:
    FROM hashicorp/terraform:$TERRAFORM_VERSION

envoy:
    FROM envoyproxy/envoy:$ENVOY_VERSION
    SAVE ARTIFACT /usr/local/bin/envoy

promtail:
    FROM grafana/promtail:$PROMTAIL_VERSION
    SAVE ARTIFACT /usr/bin/promtail

snyk-linux:
    FROM snyk/snyk:linux
    SAVE ARTIFACT /usr/local/bin/snyk

# needed for +snyk-helm, as the target is based on alpine/helm,
# and this is the only time we need a alpine-based snyk binary
snyk-alpine:
    FROM snyk/snyk:alpine
    SAVE ARTIFACT /usr/local/bin/snyk

helm:
    FROM alpine/helm:$HELM_VERSION
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
