SHELL := /bin/sh
PYTHON ?= python
TASKS := $(PYTHON) scripts/tasks.py

.DEFAULT_GOAL := help

.PHONY: help up down test lint load chaos sbom proto smoke hooks-install

help:
	@echo "Available targets:"
	@echo "  make up           - start the local development stack"
	@echo "  make down         - stop the local development stack"
	@echo "  make test         - run the current lightweight test suite"
	@echo "  make lint         - run repo lint checks"
	@echo "  make load         - run the k6 smoke load scenario"
	@echo "  make chaos        - restart the smoke auth service and re-run smoke"
	@echo "  make sbom         - generate an SBOM for the repo with syft"
	@echo "  make proto        - lint and generate protobuf code"
	@echo "  make smoke        - verify the local stack returns APPROVE"
	@echo "  make hooks-install - point git at the repo-managed hook directory"

up:
	$(TASKS) up

down:
	$(TASKS) down

test:
	$(TASKS) test

lint:
	$(TASKS) lint

load:
	$(TASKS) load

chaos:
	$(TASKS) chaos

sbom:
	$(TASKS) sbom

proto:
	$(TASKS) proto

smoke:
	$(TASKS) smoke

hooks-install:
	git config core.hooksPath .githooks
	@echo "Configured git hooks to use .githooks/"
