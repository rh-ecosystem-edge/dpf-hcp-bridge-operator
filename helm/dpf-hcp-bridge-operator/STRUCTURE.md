# Helm Chart Structure

This document describes the directory structure and purpose of each file in the DPF-HCP Bridge Operator Helm chart.

## Directory Tree

```
helm/dpf-hcp-bridge-operator/
├── Chart.yaml                      # Chart metadata
├── values.yaml                     # Default configuration values
├── values-production.yaml          # Production configuration preset
├── values-development.yaml         # Development configuration preset
├── .helmignore                     # Files to exclude from package
├── README.md                       # User documentation
│
├── crds/                           # Custom Resource Definitions
│   └── provisioning.dpu.hcp.io_dpfhcpbridges.yaml
│
├── templates/                      # Kubernetes resource templates
│   ├── _helpers.tpl               # Template helper functions
│   ├── serviceaccount.yaml        # ServiceAccount for operator pod
│   ├── clusterrole.yaml           # RBAC permissions
│   ├── clusterrolebinding.yaml    # RBAC role binding
│   ├── configmap-images.yaml      # BlueField image mappings
│   ├── deployment.yaml            # Operator deployment
│   └── NOTES.txt                  # Post-install notes
│
└── examples/                       # Example configurations
    ├── dpfhcpbridge-basic.yaml    # Basic CR example
    ├── dpfhcpbridge-ha.yaml       # HA CR example
    └── secrets-example.yaml       # Required secrets
```

## File Purposes

### Core Chart Files

| File | Purpose |
|------|---------|
| `Chart.yaml` | Chart metadata: name, version (0.1.0), appVersion (v0.1.0), description, keywords, maintainers |
| `values.yaml` | Default configuration values with inline documentation |
| `values-production.yaml` | Production-optimized preset: HA (2 replicas), higher resources, master node placement |
| `values-development.yaml` | Development-optimized preset: debug logging, lower resources, latest tag, Always pull |
| `.helmignore` | Patterns for files to exclude when packaging (*.md, examples/, etc.) |
| `README.md` | Comprehensive user documentation: installation, configuration, usage, troubleshooting |

### CRD Directory

| File | Purpose |
|------|---------|
| `crds/provisioning.dpu.hcp.io_dpfhcpbridges.yaml` | DPFHCPBridge Custom Resource Definition (installed before templates) |

### Template Directory

| File | Purpose |
|------|---------|
| `templates/_helpers.tpl` | Helm template helper functions (see below) |
| `templates/serviceaccount.yaml` | ServiceAccount that operator pod runs as |
| `templates/clusterrole.yaml` | RBAC ClusterRole defining operator permissions |
| `templates/clusterrolebinding.yaml` | Binds ClusterRole to ServiceAccount |
| `templates/configmap-images.yaml` | ConfigMap `ocp-bluefield-images` for OCP→BlueField image mappings |
| `templates/deployment.yaml` | Operator Deployment with health probes, security contexts, resources |
| `templates/NOTES.txt` | Post-installation instructions displayed after `helm install` |

**Note:** Namespace creation is handled by the `--create-namespace` flag during installation, not by a template.

### Examples Directory

| File | Purpose |
|------|---------|
| `examples/dpfhcpbridge-basic.yaml` | Basic DPFHCPBridge CR with minimal configuration |
| `examples/dpfhcpbridge-ha.yaml` | HA DPFHCPBridge CR with HighlyAvailable control plane |
| `examples/secrets-example.yaml` | Example pull secret and SSH key secret configuration |

## Template Helper Functions

Located in `templates/_helpers.tpl`:

| Helper | Purpose | Example Output |
|--------|---------|----------------|
| `dpf-hcp-bridge-operator.name` | Chart name | `dpf-hcp-bridge-operator` |
| `dpf-hcp-bridge-operator.fullname` | Full resource name (uses release name) | `dpf-hcp-bridge-operator` |
| `dpf-hcp-bridge-operator.chart` | Chart name + version | `dpf-hcp-bridge-operator-0.1.0` |
| `dpf-hcp-bridge-operator.labels` | Standard labels for all resources | `app.kubernetes.io/name`, `helm.sh/chart`, etc. |
| `dpf-hcp-bridge-operator.selectorLabels` | Pod selector labels | `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `control-plane: controller-manager` |
| `dpf-hcp-bridge-operator.serviceAccountName` | ServiceAccount name (defaults to release name if not set in values) | `dpf-hcp-bridge-operator` |
| `dpf-hcp-bridge-operator.namespace` | Namespace name | `dpf-hcp-bridge-system` |
| `dpf-hcp-bridge-operator.image` | Full image reference | `quay.io/lhadad/dpf-hcp-bridge-operator:v0.1.0` |

## Resources Created

When you install this Helm chart with release name `dpf-hcp-bridge-operator`, it creates:

| Resource Type | Name | Namespace | Scope |
|--------------|------|-----------|-------|
| ServiceAccount | `dpf-hcp-bridge-operator` | `dpf-hcp-bridge-system` | Namespaced |
| ClusterRole | `dpf-hcp-bridge-operator-manager-role` | - | Cluster |
| ClusterRoleBinding | `dpf-hcp-bridge-operator-manager-rolebinding` | - | Cluster |
| ConfigMap | `ocp-bluefield-images` | `dpf-hcp-bridge-system` | Namespaced |
| Deployment | `dpf-hcp-bridge-operator` | `dpf-hcp-bridge-system` | Namespaced |
| CustomResourceDefinition | `dpfhcpbridges.provisioning.dpu.hcp.io` | - | Cluster |

**Note:** The namespace `dpf-hcp-bridge-system` is created using the `--create-namespace` flag during installation.

## Values File Comparison

| Configuration | values.yaml (Default) | values-development.yaml | values-production.yaml |
|--------------|----------------------|-------------------------|------------------------|
| **Image Tag** | `v0.1.0` | `latest` | `v0.1.0` |
| **Pull Policy** | `Always` | `Always` | `IfNotPresent` |
| **Replicas** | `1` | `1` | `2` |
| **Log Level** | `info` | `debug` | `info` |
| **CPU Limit** | `500m` | `200m` | `1000m` |
| **Memory Limit** | `512Mi` | `256Mi` | `1Gi` |
| **CPU Request** | `100m` | `50m` | `200m` |
| **Memory Request** | `128Mi` | `128Mi` | `256Mi` |
| **Node Placement** | `master` | `master` | `master` with zone anti-affinity |
| **BlueField Images** | Empty | Empty | Placeholder examples |

## Template Rendering Examples

To see what gets rendered from templates:

```bash
# Render all templates with default values
helm template dpf-hcp-bridge-operator ./helm/dpf-hcp-bridge-operator

# Render with production values
helm template dpf-hcp-bridge-operator ./helm/dpf-hcp-bridge-operator \
  -f ./helm/dpf-hcp-bridge-operator/values-production.yaml

# Render specific template
helm template dpf-hcp-bridge-operator ./helm/dpf-hcp-bridge-operator \
  -s templates/deployment.yaml
```

## Naming Conventions

All resources follow consistent naming based on the release name:

- **Release name:** `dpf-hcp-bridge-operator` (recommended)
- **Deployment:** `{{ .Release.Name }}` → `dpf-hcp-bridge-operator`
- **ServiceAccount:** `{{ .Release.Name }}` (if `serviceAccount.name` is empty) → `dpf-hcp-bridge-operator`
- **ClusterRole:** `{{ .Release.Name }}-manager-role` → `dpf-hcp-bridge-operator-manager-role`
- **ClusterRoleBinding:** `{{ .Release.Name }}-manager-rolebinding` → `dpf-hcp-bridge-operator-manager-rolebinding`

This ensures no naming conflicts when installing multiple releases with different names.
