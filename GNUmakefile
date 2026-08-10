default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

# dev produces a binary Terraform can be pointed at, plus the CLI config that points it there.
# Both land in bin/, which is gitignored: $GOBIN moves with the Go toolchain under a version
# manager, and ~/.terraformrc is read-only wherever a config manager owns it.
dev:
	mkdir -p bin
	go build -o bin/terraform-provider-pmon .
	printf 'provider_installation {\n  dev_overrides {\n    "ridi-oss/pmon" = "%s"\n  }\n  direct {}\n}\n' '$(CURDIR)/bin' > bin/dev.tfrc
	@echo 'built. now: export TF_CLI_CONFIG_FILE=$(CURDIR)/bin/dev.tfrc'

lint:
	golangci-lint run

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate dev
