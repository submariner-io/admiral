BASE_BRANCH ?= release-0.12
export BASE_BRANCH

ifneq (,$(DAPPER_HOST_ARCH))

# Running in Dapper

include $(SHIPYARD_DIR)/Makefile.inc

TARGETS := $(shell ls -p scripts | grep -v -e /)

CLUSTER_SETTINGS_FLAG = --settings $(DAPPER_SOURCE)/.shipyard.e2e.yml
override CLUSTERS_ARGS = $(CLUSTER_SETTINGS_FLAG)
override E2E_ARGS += --nolazy_deploy cluster1 cluster2
override UNIT_TEST_ARGS += test/e2e

deploy: clusters
	./scripts/$@

e2e: vendor/modules.txt deploy
	./scripts/$@ $(E2E_ARGS)
	
.PHONY: $(TARGETS) test

else

# Not running in Dapper

Makefile.dapper:
	@echo Downloading $@
	@curl -sfLO https://raw.githubusercontent.com/submariner-io/shipyard/$(BASE_BRANCH)/$@

include Makefile.dapper

endif

# Disable rebuilding Makefile
Makefile Makefile.inc: ;
