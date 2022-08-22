SHELL=/bin/bash -e -o pipefail
PWD = $(shell pwd)

# constants
GOLANGCI_VERSION = 1.48.0
CONTAINER_REGISTRY = reg.infra.ske.eu01.stackit.cloud/ske
CONTAINER_TAG = dev
CRD_OPTIONS ?= "crd:trivialVersions=true"
KUBERNETES_VERSION = 1.21.x
ENVOY_VERSION = 1.21.1

# variables for packer build
OS_PROJECT_ID ?= UNSET
OS_NETWORK_ID ?= UNSET
OS_FLOATING_NETWORK_ID ?= UNSET
OS_SECURITY_GROUP_ID ?= UNSET
OS_SOURCE_IMAGE ?= UNSET
YAWOLLET_VERSION ?= UNSET
SOURCE_VERSION ?= local-build
BUILD_TYPE ?= local
BUILD_NUMBER ?= -1

all: git-hooks ## Initializes all tools and files

out:
	@mkdir -pv "$(@)"

test-build: ## Tests whether the code compiles
	@go build -o /dev/null ./...

build: out/bin ## Builds all binaries

container-yawol-cloud-controller: ## Builds docker image
	docker build --target yawol-cloud-controller -t $(CONTAINER_REGISTRY)/yawol-cloud-controller:$(CONTAINER_TAG) .

container-yawol-controller: ## Builds docker image
	docker build --target yawol-controller -t $(CONTAINER_REGISTRY)/yawol-controller:$(CONTAINER_TAG) .

validate-image-yawollet:
	$(PACKER) fmt -check -diff image/alpine-yawol.pkr.hcl
	$(PACKER) validate -syntax-only \
		-var 'os_project_id=UNSET' \
		-var 'source_image=UNSET' \
		-var 'image_tags=[]' \
		-var 'image_version=UNSET' \
		-var 'network_id=UNSET' \
		-var 'floating_network_id=UNSET' \
		-var 'security_group_id=UNSET' \
		-var 'image_visibility=private' \
		image/alpine-yawol.pkr.hcl

# Builds yawollet VM image
build-image-yawollet: build get-envoy get-packer
	$(PACKER) build \
		-var 'source_image=$(OS_SOURCE_IMAGE)' \
		-var 'image_version=$(YAWOLLET_VERSION)' \
		-var 'image_tags=["git-$(SOURCE_VERSION)", "build-$(BUILD_TYPE)-$(BUILD_NUMBER)", "$(YAWOLLET_VERSION)"]' \
		-var 'os_project_id=$(OS_PROJECT_ID)' \
		-var 'network_id=$(OS_NETWORK_ID)' \
		-var 'floating_network_id=$(OS_FLOATING_NETWORK_ID)' \
		-var 'security_group_id=$(OS_SECURITY_GROUP_ID)' \
		-var 'image_visibility=private' \
		-on-error=ask \
		image/alpine-yawol.pkr.hcl

GO_BUILD = mkdir -pv "$(@)" && go build -ldflags="-w -s" -o "$(@)" ./...
.PHONY: out/bin
out/bin:
	$(GO_BUILD)

git-hooks:
	@git config --local core.hooksPath .githooks/

download: ## Downloads the dependencies
	@go mod download

fmt: ## Formats all code with go fmt
	@go fmt ./...

GOLANGCI_LINT = bin/golangci-lint-$(GOLANGCI_VERSION)
$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | bash -s -- -b bin v$(GOLANGCI_VERSION)
	@mv bin/golangci-lint "$(@)"

OS = linux
ifeq ($(shell uname -s),Darwin)
	OS = darwin
endif
ENVOY = bin/envoy-$(ENVOY_VERSION)
ENVOY_PATH=envoy-v$(ENVOY_VERSION)-$(OS)-amd64
$(ENVOY): ## Download envoy binary for linux
	mkdir -p bin
	wget -qO- https://github.com/tetratelabs/archive-envoy/releases/download/v$(ENVOY_VERSION)/$(ENVOY_PATH).tar.xz | tar xfvJ - -C bin
	ln -sf "$(ENVOY_PATH)/bin/envoy" "$(@)"
	ln -sf "envoy-$(ENVOY_VERSION)" "bin/envoy"

get-envoy: $(ENVOY) ## alias to install latest envoy version

PACKER_VERSION = 1.8.3
PACKER_SHA256 = "0587f7815ed79589cd9c2b754c82115731c8d0b8fd3b746fe40055d969facba5"
PACKER_DIR=bin/packer
PACKER_ZIP = "packer_$(PACKER_VERSION)_$(OS)_amd64.zip"
PACKER = "$(PACKER_DIR)/packer"

bin/packer/packer:
	mkdir -p $(PACKER_DIR)
	wget -P $(PACKER_DIR) "https://releases.hashicorp.com/packer/$(PACKER_VERSION)/$(PACKER_ZIP)"
	echo "$(PACKER_SHA256) $(PACKER_DIR)/$(PACKER_ZIP)" | sha256sum --check
	unzip $(PACKER_DIR)/$(PACKER_ZIP) -d $(PACKER_DIR)

get-packer: bin/packer/packer

lint: fmt $(GOLANGCI_LINT) download ## Lints all code with golangci-lint
	@$(GOLANGCI_LINT) run

lint-reports: out/lint.xml

.PHONY: out/lint.xml
out/lint.xml: $(GOLANGCI_LINT) out download
	$(GOLANGCI_LINT) run ./... --out-format checkstyle | tee "$(@)"

RUN_ENVTEST = bin/setup-envtest --bin-dir $(PWD)/bin
SOURCE_ENVTEST = eval `$(RUN_ENVTEST) use -p env $(KUBERNETES_VERSION)`
GO_TEST = PATH=$(PWD)/bin:$$PATH go test ./...
test: crd $(ENVOY) bin/setup-envtest ## Runs all tests
	@$(SOURCE_ENVTEST) && $(GO_TEST)

test-reports: out/report.json

.PHONY: out/report.json
out/report.json: out bin/setup-envtest crd
	@$(SOURCE_ENVTEST) && go test ./... -coverprofile=out/cover.out --json | tee "$(@)"

clean-envtest:
	@$(RUN_ENVTEST) cleanup "<=$(KUBERNETES_VERSION)" 2> /dev/null || echo "skipping envtest cleanup"

clean: clean-envtest ## Cleans up everything
	@rm -rf bin out

crd: charts/yawol-controller/crds

.PHONY: charts/yawol-controller/crds
charts/yawol-controller/crds: bin/controller-gen
	bin/controller-gen $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config="$(@)"

generate: bin/controller-gen
	bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

install: charts/yawol-controller/crds ## Installs crds in kubernetes cluster
	kubectl apply -f charts/yawol-controller/crds

ci: lint-reports test-reports

help:
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@grep -E '^[ a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ''

# Go dependencies versioned through tools.go
GO_DEPENDENCIES = sigs.k8s.io/controller-tools/cmd/controller-gen sigs.k8s.io/controller-runtime/tools/setup-envtest

define make-go-dependency
# target template for go tools, can be referenced e.g. via /bin/<tool>
bin/$(notdir $1):
	GOBIN=$(PWD)/bin go install $1
endef

# this creates a target for each go dependency to be referenced in other targets
$(foreach dep, $(GO_DEPENDENCIES), $(eval $(call make-go-dependency, $(dep))))
