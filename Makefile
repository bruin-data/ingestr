.ONESHELL:
.PHONY: test lint format ftl test-ci lint-ci build upload-release

venv: venv/touchfile

venv/touchfile: requirements-dev.txt requirements.txt
	test -d venv || python3 -m venv venv
	. venv/bin/activate; $(MAKE) deps
	touch venv/touchfile

deps:
	uv pip install -r requirements-dev.txt

deps-ci:
	uv pip install --system -r requirements-dev.txt

test-ci:
	TESTCONTAINERS_RYUK_DISABLED=true pytest -n auto -x -rP -vv --tb=short --durations=10 --cov=ingestr --no-cov-on-fail

test: venv
	. venv/bin/activate; $(MAKE) test-ci

test-specific: venv
	. venv/bin/activate; pytest -rP -vv --tb=short --capture=no -k $(test)

lint-ci:
	ruff check ingestr --fix && ruff format ingestr
	mypy --config-file pyproject.toml --explicit-package-bases ingestr

lint: venv
	. venv/bin/activate; $(MAKE) lint-ci

lint-docs:
	vale docs --glob='!**/.vitepress/**'

tl: test lint

build:
	rm -rf dist && python3 -m build

upload-release:
	twine upload --verbose dist/*
