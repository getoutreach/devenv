APP := devenv
OSS := false
_ := $(shell ./scripts/bootstrap-lib.sh) 

include .bootstrap/root/Makefile

###Block(targets)
.PHONY: install
install: build
	@devenvPath="$$(command -v devenv)"; if [[ -w "$$devenvPath" ]]; then cp -v ./bin/devenv "$$devenvPath"; else sudo cp -v ./bin/devenv "$$devenvPath"; fi

docker-build-override:
	docker buildx build --platform "linux/amd64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" --load .

.PHONY: docker-push
docker-push:
	docker buildx build --platform "linux/amd64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" --push .
###EndBlock(targets)
