# Based around the auto-documented Makefile:
# http://marmelab.com/blog/2016/02/29/auto-documented-makefile.html

.PHONY: build
build: ## Builds artifact
	$(call print-target)
	@mkdir -p ./build
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o ./build/agentd

.PHONY: help
.DEFAULT_GOAL := help
help: ## Prints this help message
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
define print-target
    @printf "Executing target: \033[36m$@\033[0m\n"
endef
