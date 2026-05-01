# Image URL to use all building/pushing image targets
IMG ?= claw-operator:latest
PROXY_IMG ?= claw-proxy:latest
PLATFORM ?= linux/amd64

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= podman

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/..." paths="./internal/..." paths="./cmd/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..." paths="./internal/..." paths="./cmd/..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test -p 1 $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager container image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= claw-operator-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac
	@mkdir -p tmp
	@$(KUBECTL) config current-context > tmp/.pre-e2e-context 2>/dev/null || true
	@$(KUBECTL) config use-context kind-$(KIND_CLUSTER)

.PHONY: reset-test-e2e
reset-test-e2e: ## Remove leftover operator resources from a previous e2e run
	@echo "Resetting e2e test state..."
	-$(MAKE) undeploy 2>/dev/null
	-$(MAKE) uninstall 2>/dev/null
	-$(KUBECTL) delete ns claw-operator --ignore-not-found

.PHONY: test-e2e
test-e2e: ## Run the e2e tests. Expected an isolated environment using Kind.
	@trap '$(MAKE) cleanup-test-e2e >/dev/null 2>&1 || true' EXIT; \
	$(MAKE) setup-test-e2e manifests generate fmt vet reset-test-e2e; \
	KIND_CLUSTER=$(KIND_CLUSTER) go test -v -timeout 15m ./test/e2e/

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@status=0; \
	$(KIND) delete cluster --name $(KIND_CLUSTER) || status=$$?; \
	if [ -f tmp/.pre-e2e-context ]; then \
		ctx=$$(cat tmp/.pre-e2e-context); \
		rm -f tmp/.pre-e2e-context; \
		if [ -n "$$ctx" ] && $(KUBECTL) config get-contexts "$$ctx" >/dev/null 2>&1; then \
			echo "Restoring kubectl context to $$ctx"; \
			$(KUBECTL) config use-context "$$ctx"; \
		fi; \
	fi; \
	exit $$status

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: build-proxy
build-proxy: fmt vet ## Build proxy binary.
	go build -o bin/proxy cmd/proxy/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. podman build --platform linux/arm64)
.PHONY: container-build
container-build: ## Build container image with the manager.
	$(CONTAINER_TOOL) build --platform=${PLATFORM} -t ${IMG} \
		--build-arg VERSION=$$(git rev-parse --short HEAD) \
		--build-arg BUILD_TIME=$$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
		-f Containerfile .

.PHONY: container-save
container-save: ## Save the container image to a tar file.
	mkdir -p tmp 2>/dev/null 
	rm -f ${OUTPUT_FILE} || true
	$(CONTAINER_TOOL) save -o ${OUTPUT_FILE} ${IMG}

.PHONY: container-push
container-push: ## Push container image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

.PHONY: container-build-proxy
container-build-proxy: ## Build container image for the credential proxy.
	$(CONTAINER_TOOL) build --platform=${PLATFORM} -t ${PROXY_IMG} -f Containerfile.proxy .

.PHONY: container-push-proxy
container-push-proxy: ## Push container image for the credential proxy.
	$(CONTAINER_TOOL) push ${PROXY_IMG}

# generate-deploy-overlay creates a temporary kustomize overlay at config/.deploy/
# that wraps config/default with image and PROXY_IMAGE overrides.
# This avoids mutating committed files (manager.yaml, kustomization.yaml).
# Usage: $(call generate-deploy-overlay,<controller-image>,<proxy-image>[,<pull-policy>])
# pull-policy defaults to IfNotPresent; dev-deploy passes Always to force re-pulls.
define generate-deploy-overlay
	@rm -rf config/.deploy && mkdir -p config/.deploy
	@printf 'apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n- ../default\nimages:\n- name: controller\n  newName: $(shell echo $(1) | cut -d: -f1)\n  newTag: $(shell echo $(1) | cut -d: -f2)\npatches:\n- path: proxy_image_patch.yaml\n  target:\n    kind: Deployment\n' > config/.deploy/kustomization.yaml
	@printf 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: controller-manager\nspec:\n  template:\n    spec:\n      containers:\n      - name: manager\n        imagePullPolicy: $(or $(3),IfNotPresent)\n        env:\n        - name: PROXY_IMAGE\n          value: "$(2)"\n        - name: IMAGE_PULL_POLICY\n          value: "$(or $(3),)"\n' > config/.deploy/proxy_image_patch.yaml
endef

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(call generate-deploy-overlay,$(IMG),$(PROXY_IMG))
	@trap 'rm -rf config/.deploy' EXIT; $(KUSTOMIZE) build config/.deploy | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dev Deployment

# Dev targets derive IMG and PROXY_IMG from REGISTRY and TAG.
# Usage: make dev-setup REGISTRY=quay.io/myuser
ifndef TAG
TAG := dev-$(shell git rev-parse --short HEAD)-$(shell date +%s)
endif

.PHONY: dev-build
dev-build: ## Build operator and proxy images for dev.
ifndef REGISTRY
	$(error REGISTRY is required. Usage: make dev-build REGISTRY=quay.io/myuser)
endif
	$(MAKE) container-build IMG=$(REGISTRY)/claw-operator:$(TAG)
	$(MAKE) container-build-proxy PROXY_IMG=$(REGISTRY)/claw-proxy:$(TAG)

.PHONY: dev-push
dev-push: ## Push operator and proxy images for dev.
ifndef REGISTRY
	$(error REGISTRY is required. Usage: make dev-push REGISTRY=quay.io/myuser)
endif
	$(MAKE) container-push IMG=$(REGISTRY)/claw-operator:$(TAG)
	$(MAKE) container-push-proxy PROXY_IMG=$(REGISTRY)/claw-proxy:$(TAG)

.PHONY: dev-deploy
dev-deploy: manifests kustomize ## Install CRDs and deploy controller for dev (uses Always pull policy).
ifndef REGISTRY
	$(error REGISTRY is required. Usage: make dev-deploy REGISTRY=quay.io/myuser)
endif
	$(MAKE) install
	$(call generate-deploy-overlay,$(REGISTRY)/claw-operator:$(TAG),$(REGISTRY)/claw-proxy:$(TAG),Always)
	@trap 'rm -rf config/.deploy' EXIT; $(KUSTOMIZE) build config/.deploy | $(KUBECTL) apply -f -
	@$(KUBECTL) rollout restart deployment -n claw-operator claw-operator-controller-manager || { echo "ERROR: rollout restart failed" >&2; false; }

.PHONY: dev-setup
dev-setup: ## Full dev setup: build, push, and deploy.
ifndef REGISTRY
	$(error REGISTRY is required. Usage: make dev-setup REGISTRY=quay.io/myuser)
endif
	$(MAKE) dev-build REGISTRY=$(REGISTRY) TAG=$(TAG)
	$(MAKE) dev-push REGISTRY=$(REGISTRY) TAG=$(TAG)
	$(MAKE) dev-deploy REGISTRY=$(REGISTRY) TAG=$(TAG)

NS ?= my-claw
CLAW ?= instance

.PHONY: wait-ready
wait-ready: ## Wait for the Claw instance to become ready and print the URL. Usage: make wait-ready NS=... [CLAW=instance]
	@echo "Waiting for Claw $(CLAW) to become ready in namespace $(NS)..."
	@$(KUBECTL) wait --for=condition=Ready claw/$(CLAW) -n $(NS) --timeout=300s
	@echo
	@echo "URL: $$($(KUBECTL) get claw $(CLAW) -n $(NS) -o jsonpath='{.status.url}')"
	@token_secret=$$($(KUBECTL) get claw $(CLAW) -n $(NS) -o jsonpath='{.status.gatewayTokenSecretRef}'); \
	echo "Token: $$($(KUBECTL) get secret $$token_secret -n $(NS) -o jsonpath='{.data.token}' | base64 -d)"

# Approve pairing by directly writing pending.json/paired.json on the PVC.
# The CLI's gateway RPC path (device.pair.approve) requires the caller to hold
# all scopes being granted — a delegation security model. When running via
# kubectl exec, the shared gateway token creates a device-less connection whose
# scopes are stripped, so the delegation check fails. This mirrors the CLI's own
# local fallback (approvePairingWithFallback in devices-cli.ts).
APPROVE_SCRIPT = var fs=require("fs"),crypto=require("crypto"),p=require("path"),dir=p.join(process.env.HOME||"/home/node",".openclaw","devices"),rid=process.env.APPROVE_RID,pf=p.join(dir,"pending.json"),af=p.join(dir,"paired.json"),pending=JSON.parse(fs.readFileSync(pf,"utf8")),e=pending[rid];if(!e){console.error("unknown requestId: "+rid);process.exit(1)}var paired;try{paired=JSON.parse(fs.readFileSync(af,"utf8"))}catch(_){paired={}}var roles=e.roles&&e.roles.length?e.roles:e.role?[e.role]:["operator"],scopes=e.scopes||[],tokens=(paired[e.deviceId]||{}).tokens||{},now=Date.now();roles.forEach(function(r){tokens[r]={token:crypto.randomBytes(32).toString("base64url"),role:r,scopes:scopes,createdAtMs:now}});paired[e.deviceId]={deviceId:e.deviceId,publicKey:e.publicKey,displayName:e.displayName,platform:e.platform,deviceFamily:e.deviceFamily,clientId:e.clientId,clientMode:e.clientMode,role:e.role,roles:roles,scopes:scopes,approvedScopes:scopes,remoteIp:e.remoteIp,tokens:tokens,createdAtMs:(paired[e.deviceId]||{}).createdAtMs||now,approvedAtMs:now};delete pending[rid];fs.writeFileSync(pf,JSON.stringify(pending,null,2));fs.writeFileSync(af,JSON.stringify(paired,null,2));console.log("Approved device "+e.deviceId.substring(0,12)+"... for roles: "+roles.join(", "))
LIST_SCRIPT = var fs=require("fs"),p=require("path"),dir=p.join(process.env.HOME||"/home/node",".openclaw","devices");try{var d=JSON.parse(fs.readFileSync(p.join(dir,"pending.json"),"utf8"));Object.values(d).sort(function(a,b){return(b.ts||0)-(a.ts||0)}).forEach(function(r){console.log(r.requestId+" "+(r.platform||"unknown")+" "+(r.clientMode||""))})}catch(_){}

.PHONY: approve-pairing
approve-pairing: ## Approve a device pairing request. Usage: make approve-pairing NS=... [CLAW=instance] [REQUEST_ID=...]
	@if [ -n "$(REQUEST_ID)" ]; then \
		$(KUBECTL) exec -n $(NS) deployment/$(CLAW) -c gateway -- \
			env APPROVE_RID="$(REQUEST_ID)" node -e '$(APPROVE_SCRIPT)'; \
	else \
		output=$$($(KUBECTL) exec -n $(NS) deployment/$(CLAW) -c gateway -- \
			node -e '$(LIST_SCRIPT)' 2>/dev/null); \
		rid=$$(echo "$$output" | head -1 | cut -d' ' -f1); \
		if [ -z "$$rid" ]; then \
			echo "No pending pairing requests found."; \
			exit 1; \
		fi; \
		echo "Found pairing request: $$rid"; \
		desc=$$(echo "$$output" | head -1 | cut -d' ' -f2-); \
		if [ -n "$$desc" ]; then echo "  Device: $$desc"; fi; \
		read -r -p "Approve? [y/N] " ans; \
		case "$$ans" in [yY]*) ;; *) echo "Aborted."; exit 0;; esac; \
		$(KUBECTL) exec -n $(NS) deployment/$(CLAW) -c gateway -- \
			env APPROVE_RID="$$rid" node -e '$(APPROVE_SCRIPT)'; \
	fi

.PHONY: dev-cleanup
dev-cleanup: ## Remove deployed controller and CRDs.
	$(MAKE) undeploy ignore-not-found=true
	$(MAKE) uninstall ignore-not-found=true

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.19.0
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.11.4

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

