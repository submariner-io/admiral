status ?= onetime
version ?= 1.14.3
kubefed ?= true

TARGETS := $(shell ls scripts)

.dapper:
	@echo Downloading dapper
	@curl -sL https://releases.rancher.com/dapper/latest/dapper-`uname -s`-`uname -m` > .dapper.tmp
	@@chmod +x .dapper.tmp
	@./.dapper.tmp -v
	@mv .dapper.tmp .dapper

$(TARGETS): .dapper
	./.dapper -m bind $@ $(status) $(version) $(kubefed)

.DEFAULT_GOAL := ci

.PHONY: $(TARGETS)