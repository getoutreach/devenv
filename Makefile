APP := devenv
OSS := true
_ := $(shell ./scripts/devbase.sh) 

include .bootstrap/root/Makefile

###Block(targets)
# E2E Flags
export SKIP_LOCALIZER=true
export TEST_OUTPUT_FORMAT=standard-verbose
export TEST_FLAGS=-v
export SKIP_DEVENV_PROVISION=true
export GO_TEST_TIMEOUT=30m0s

LDFLAGS += -X k8s.io/component-base/version.gitMajor=1
LDFLAGS += -X k8s.io/component-base/version.gitMinor=23
LDFLAGS += -X k8s.io/component-base/version.gitVersion=v1.23.5
LDFLAGS += -X k8s.io/component-base/version.gitCommit=272114478c66b8250050dd68d4719c46c2ab2088

# Note: We rm here because M1 macs get angry about copying new files onto
# existing ones. Probably because of some signature thing. Who knows.
.PHONY: install
install: build
	@devenvPath="$$(command -v devenv)"; rm "$$devenvPath"; if [[ -w "$$devenvPath" ]]; then cp -v ./bin/devenv "$$devenvPath"; else sudo cp -v ./bin/devenv "$$devenvPath"; fi

docker-build-dev:
	DOCKER_BUILDKIT=1 docker build --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" .
###EndBlock(targets)
