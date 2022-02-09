APP := devenv
OSS := true
_ := $(shell ./scripts/bootstrap-lib.sh) 

###Deviation(make): Overwrite the e2e target
.PHONY: e2e
e2e::
	@echo Use make e2e-override
	@exit 1


include .bootstrap/root/Makefile

###Block(targets)
.PHONY: e2e-override
e2e-override:
	@echo "Hello, world!"

# Note: We rm here because M1 macs get angry about copying new files onto
# existing ones. Probably because of some signature thing. Who knows.
.PHONY: install
install: build
	@devenvPath="$$(command -v devenv)"; rm "$$devenvPath"; if [[ -w "$$devenvPath" ]]; then cp -v ./bin/devenv "$$devenvPath"; else sudo cp -v ./bin/devenv "$$devenvPath"; fi

.PHONY: docker-build-override
docker-build-override:
	docker buildx build --platform "linux/amd64,linux/arm64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" .

.PHONY: docker-push-override
docker-push-override:
	docker buildx build --platform "linux/amd64,linux/arm64" --ssh default -t "gcr.io/outreach-docker/devenv:$(APP_VERSION)" --push .
###EndBlock(targets)
