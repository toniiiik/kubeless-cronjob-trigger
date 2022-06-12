GO = go
GO_FLAGS =
GOFMT = gofmt
KUBECFG = kubecfg
DOCKER = docker
CONTROLLER_IMAGE = kubeless-controller-manager:latest
CRONJOB_CONTROLLER_IMAGE = repodx.goldmann.sk/kubeless/cronjob-trigger-controller:1.24.0
OS = linux
ARCH = amd64
BUNDLES = bundles
GO_PACKAGES = ./cmd/... ./pkg/...
GO_FILES := $(shell find $(shell $(GO) list -f '{{.Dir}}' $(GO_PACKAGES)) -name \*.go)

export KUBECFG_JPATH := $(CURDIR)/ksonnet-lib
export PATH := $(PATH):$(CURDIR)/bats/bin

.PHONY: all

default: binary

binary:
	CGO_ENABLED=1 ./script/binary

cronjob-controller-build:
	./script/binary-controller -os=$(OS) -arch=$(ARCH) cronjob-controller github.com/kubeless/cronjob-trigger/cmd/cronjob-trigger-controller

cronjob-controller-image: docker/cronjob-controller
	$(DOCKER) build -t $(CRONJOB_CONTROLLER_IMAGE) $<

docker/cronjob-controller: cronjob-controller-build
	cp $(BUNDLES)/kubeless_$(OS)-$(ARCH)/cronjob-controller $@

update:
	./hack/update-codegen.sh

test:
	$(GO) test $(GO_FLAGS) $(GO_PACKAGES)

validation:
	./script/validate-lint
	./script/validate-gofmt
	./script/validate-git-marks

integration-tests:
	./script/integration-tests minikube deployment
	./script/integration-tests minikube basic

fmt:
	$(GOFMT) -s -w $(GO_FILES)

bats:
	git clone --depth=1 https://github.com/sstephenson/bats.git

ksonnet-lib:
	git clone --depth=1 https://github.com/ksonnet/ksonnet-lib.git

.PHONY: bootstrap
bootstrap: bats ksonnet-lib

	go get github.com/mitchellh/gox

	@if ! which kubecfg >/dev/null; then \
	sudo wget -q -O /usr/local/bin/kubecfg https://github.com/ksonnet/kubecfg/releases/download/v0.9.0/kubecfg-$$(go env GOOS)-$$(go env GOARCH); \
	sudo chmod +x /usr/local/bin/kubecfg; \
	fi

	@if ! which kubectl >/dev/null; then \
	KUBECTL_VERSION=$$(wget -qO- https://storage.googleapis.com/kubernetes-release/release/stable.txt); \
	sudo wget -q -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/$$KUBECTL_VERSION/bin/$$(go env GOOS)/$$(go env GOARCH)/kubectl; \
	sudo chmod +x /usr/local/bin/kubectl; \
	fi
