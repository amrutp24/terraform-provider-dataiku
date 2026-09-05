BINARY  := terraform-provider-dataiku
VERSION ?= dev

.PHONY: default
default: build

.PHONY: build
build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

.PHONY: install
install:
	go install -ldflags "-X main.version=$(VERSION)" .

.PHONY: test
test: test-module
	go test -cover -race ./...

# The bootstrap module creates nothing, so its tests are plan-time assertions
# on the rendered script: no credentials, no cloud, a couple of seconds.
.PHONY: test-module
test-module:
	cd modules/dss-bootstrap && terraform init -backend=false >/dev/null && terraform test

# Acceptance tests create and delete real objects on the instance named by
# DATAIKU_HOST. Never point them at production.
.PHONY: testacc
testacc:
	TF_ACC=1 go test -v -cover -timeout 30m ./internal/provider/

.PHONY: fmt
fmt:
	gofmt -w .
	terraform fmt -recursive ./examples/

.PHONY: lint
lint:
	gofmt -l .
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

.PHONY: docs
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate \
		--provider-name dataiku \
		--rendered-provider-name Dataiku

.PHONY: clean
clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist/
