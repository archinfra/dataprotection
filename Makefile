GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
GOFMT ?= $(shell command -v gofmt 2>/dev/null || echo /usr/local/go/bin/gofmt)
VERSION ?= $(shell cat VERSION)
LOCALBIN ?= $(CURDIR)/bin
ifeq ($(OS),Windows_NT)
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen.exe
else
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
endif

.PHONY: all
all: fmt mod-tidy test

.PHONY: controller-gen
controller-gen:
	@test -x "$(CONTROLLER_GEN)" || { \
		echo "installing controller-gen to $(LOCALBIN)"; \
		mkdir -p "$(LOCALBIN)"; \
		GOBIN="$(LOCALBIN)" $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5; \
	}

.PHONY: fmt
fmt:
	find . -name '*.go' -print0 | xargs -0 $(GOFMT) -w

.PHONY: mod-tidy
mod-tidy:
	$(GO) mod tidy

.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object paths="./..."

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) crd paths="./api/v1alpha1/..." output:crd:artifacts:config=config/crd/bases

.PHONY: test
test:
	$(GO) test ./...

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -o bin/data-protection-operator .

.PHONY: docker-build
docker-build:
	docker build -t data-protection-operator:$(VERSION) .

.PHONY: installer
installer:
	./build.sh --arch amd64
