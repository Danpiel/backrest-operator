FROM python:3.12-slim

RUN useradd -r -u 65532 -m nonroot
WORKDIR /app

COPY pyproject.toml README.md LICENSE ./
COPY src ./src

RUN pip install --no-cache-dir . \
 && backrest-operator --help >/dev/null 2>&1 || true

USER 65532:65532
EXPOSE 8080 8081 9443
ENTRYPOINT ["backrest-operator"]
