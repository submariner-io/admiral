status ?= onetime
version ?= 1.14.6

TARGETS := $(shell ls scripts)

.dapper:
	@echo Downloading dapper
	@curl -sL https://releases.rancher.com/dapper/latest/dapper-`uname -s`-`uname -m` > .dapper.tmp
	@@chmod +x .dapper.tmp
	@./.dapper.tmp -v
	@mv .dapper.tmp .dapper

shell:
	./.dapper -m bind -s

$(TARGETS): .dapper
	./.dapper -m bind $@ $(status) $(version)

.DEFAULT_GOAL := ci

.PHONY: $(TARGETS)
