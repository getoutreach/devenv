APP := devenv
OSS := true
_ := $(shell ./scripts/devbase.sh) 

include .bootstrap/root/Makefile

###Block(targets)
.PHONY: e2e-override
e2e-override:
	TEST_OUTPUT_FORMAT=standard-verbose TEST_FLAGS=-v TEST_TAGS=or_e2e ./scripts/shell-wrapper.sh test.sh

# Note: We rm here because M1 macs get angry about copying new files onto
# existing ones. Probably because of some signature thing. Who knows.
.PHONY: install
install: build
	@devenvPath="$$(command -v devenv)"; rm "$$devenvPath"; if [[ -w "$$devenvPath" ]]; then cp -v ./bin/devenv "$$devenvPath"; else sudo cp -v ./bin/devenv "$$devenvPath"; fi

docker-build-dev:
	DOCKER_BUILDKIT=1 docker build --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" .

.PHONY: docker-build-override
docker-build-override:
	docker buildx build --platform "linux/amd64,linux/arm64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" .

.PHONY: docker-push-override
docker-push-override:
	docker buildx build --platform "linux/amd64,linux/arm64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" --push .
###EndBlock(targets)
