.PHONY: check-install
check-install:
ifeq ($(shell which terraform),)
	@echo "ERROR: Could not find 'terraform' in PATH"; exit 1
endif

.PHONY: init-no-backend
init-no-backend: check-install
	terraform init --backend=false

.PHONY: validate
validate: init-no-backend
	terraform validate
