# Backrest Operator Helm Chart

Installs the Backrest Kubernetes operator, optional MCP server, RBAC roles, validating webhooks, and monitoring resources.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- CRDs applied from `crds/` before or during install (`helm install --skip-crds` if CRDs are pre-applied)
- cert-manager (recommended when `operator.webhook.certManager.enabled` is true)
- Prometheus Operator and/or VictoriaMetrics Operator (optional, for scrape CRs and alert rules)

## Install

```bash
helm install backrest-operator ./charts/backrest-operator \
  --namespace backrest-system \
  --create-namespace
```

Apply CRDs separately:

```bash
kubectl apply -f crds/
```

## Configuration

See [values.yaml](./values.yaml) for the full surface. Key areas:

| Area | Values prefix |
|------|----------------|
| Operator deployment | `operator.*` |
| MCP server | `mcp.*` |
| Default Backrest image | `backrest.image.*` |
| Monitoring | `monitoring.*` |
| User-facing RBAC | `rbac.userRoles.*` |

## Uninstall

```bash
helm uninstall backrest-operator --namespace backrest-system
```

CRDs are not removed automatically.
