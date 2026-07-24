.PHONY: test lint install docker chart-package

install:
	pip install -e ".[dev]"

test:
	pytest -q

lint:
	python -m compileall -q src

docker:
	docker build -t backrest-operator:0.1.0 -f Dockerfile .
	docker build -t backrest-mcp:0.1.0 -f Dockerfile.mcp .

chart-package:
	helm package charts/backrest-operator -d dist
