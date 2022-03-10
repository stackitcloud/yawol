SHELL=/bin/bash -e -o pipefail
PWD = $(shell pwd)

# constants
GOLANGCI_VERSION = 1.42.1
CRD_OPTIONS ?= "crd:trivialVersions=true"
KUBERNETES_VERSION = 1.22.x

all: git-hooks ## Initializes all tools and files

out:
	@mkdir -pv "$(@)"

test-build: ## Tests whether the code compiles
	@go build -o /dev/null ./...

build: out/bin ## Builds all binaries

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

lint: fmt $(GOLANGCI_LINT) download ## Lints all code with golangci-lint
	@$(GOLANGCI_LINT) run

lint-reports: out/lint.xml

.PHONY: out/lint.xml
out/lint.xml: $(GOLANGCI_LINT) out download
	$(GOLANGCI_LINT) run ./... --out-format checkstyle | tee "$(@)"

RUN_ENVTEST = bin/setup-envtest --bin-dir $(PWD)/bin
SOURCE_ENVTEST = eval `$(RUN_ENVTEST) use -p env $(KUBERNETES_VERSION)`
test: crd bin/setup-envtest ## Runs all tests
	@$(SOURCE_ENVTEST) && go test ./...

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
