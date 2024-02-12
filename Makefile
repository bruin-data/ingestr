.ONESHELL:
.PHONY: test lint format ftl test-ci lint-ci build

venv: venv/touchfile

venv/touchfile: requirements.txt
	test -d venv || python3 -m venv venv
	. venv/bin/activate; pip install -r requirements.txt
	touch venv/touchfile

install-deps:
	pip install -r requirements.txt

test-ci:
	pytest -rP -vv --tb=short --cov=ingestr --no-cov-on-fail

test: venv
	. venv/bin/activate; $(MAKE) test-ci

test-specific: venv
	. venv/bin/activate; pytest -rP -vv --tb=short --cov=ingestr --no-cov-on-fail -k $(test)

lint-ci:
	ruff ingestr --fix && ruff format ingestr
	mypy  --explicit-package-bases ingestr --config-file pyproject.toml

lint: venv
	. venv/bin/activate; $(MAKE) lint-ci

tl: test lint

build:
	python3 -m build

install-locally:
	python3 -m build
