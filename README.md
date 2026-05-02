
# kubepose

> A minimalist tool to convert [Compose specification](https://compose-spec.io/) files to Kubernetes manifests

## Why kubepose?

kubepose provides a simpler alternative to [kompose](https://kompose.io/), focusing solely on converting Compose specifications to Kubernetes YAML files with:

- ✨ **Zero Configuration** Your compose file is the only input needed
- 🎯 **Predictable Output** Generates clean, standard Kubernetes manifests
- 🔒 **Immutable by Default** Secrets and configmaps are created immutably

## Installation

```bash
# Using go install
go install github.com/middle-management/kubepose/cmd/kubepose@latest

# Or download latest release from https://github.com/middle-management/kubepose/releases
curl -L "https://github.com/middle-management/kubepose/releases/latest/download/kubepose-$(uname -s)-$(uname -m)" -o kubepose

# Make it executable
chmod +x kubepose

# Move it somewhere in your PATH
sudo mv kubepose /usr/local/bin/
```

### Verify the signature of the release

The releases are signed using [cosign](https://github.com/sigstore/cosign). To verify the signature, you need to [install cosign first](https://docs.sigstore.dev/cosign/system_config/installation/).

```bash
# first download the certificate and signature files
curl -L "https://github.com/middle-management/kubepose/releases/latest/download/kubepose-$(uname -s)-$(uname -m).pem" -o kubepose-$(uname -s)-$(uname -m).pem
curl -L "https://github.com/middle-management/kubepose/releases/latest/download/kubepose-$(uname -s)-$(uname -m).sig" -o kubepose-$(uname -s)-$(uname -m).sig

# then use cosign to verify the signature
cosign verify-blob \
  --certificate kubepose-$(uname -s)-$(uname -m).pem \
  --signature kubepose-$(uname -s)-$(uname -m).sig \
  --certificate-identity "https://github.com/middle-management/kubepose/.github/workflows/release.yaml@refs/tags/<tag-version>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  kubepose-$(uname -s)-$(uname -m)
```

## Quick Start

```bash
# Convert compose files to K8s manifests
kubepose convert

# Specify input files explicitly
kubepose convert -f compose.yaml -f compose.prod.yaml

# Use with kubectl
kubepose convert | kubectl apply -n my-ns -f -

# Use with specific profiles
kubepose convert -p prod
```

kubepose follows the same file lookup order as `docker compose`:
```
compose.yaml
compose.yml
docker-compose.yaml
docker-compose.yml
```

## Examples

The tests in the `testdata` directory are integration tests which also work as examples of various Compose configurations and their corresponding Kubernetes output. Each feature has its own directory with a `compose.yaml` and its converted Kubernetes manifests in the `TestConvert` directory. See [`testdata/simple/compose.yaml`](testdata/simple/compose.yaml) and its corresponding [`testdata/TestConvert/simple/k8s.yaml`](testdata/TestConvert/simple/k8s.yaml) as an example.

## Key Features

- 🎮 **Simple CLI** - Single command with familiar `-f` and `-p` flags
- 🚀 **Application First** - Focus on deploying applications, not managing clusters
- 🔄 **Standard Conversion** - Predictable mapping to Kubernetes resources
- 📦 **No Dependencies** - Single binary with zero runtime requirements
- 🎯 **Targeted Scope** - Focused purely on Compose to Kubernetes conversion

## Supported Resources

### Core Workloads

| Feature | Status | Description |
|---------|:------:|-------------|
| Deployments | ✅ | Default workload type |
| DaemonSets | ✅ | Enable with `deploy.mode: global` |
| Multi-Container Pods | ✅ | Group via `kubepose.service.group` |
| Init Containers | ✅ | Mark with `kubepose.container.type: init` |
| Sidecar Containers | ✅ | Init containers with `restart: always` |
| StatefulSets | ✅ | Enable with `kubepose.workload: StatefulSet` |
| CronJobs | ✅ | Enable with `kubepose.cronjob.schedule: "<cron>"` |
| HorizontalPodAutoscaler | ✅ | Enable with `kubepose.hpa.{minReplicas,maxReplicas,targetCPUUtilization}` |

### Container Configuration

| Feature | Status | Description |
|---------|:------:|-------------|
| Image & Tags | ✅ | Full support for image references |
| Commands | ✅ | Both `command` and `entrypoint` |
| Update Strategies | ✅ | Configurable update behavior |
| Environment | ✅ | Variables and values |
| Working Directory | ✅ | Via `working_dir` |
| Shell Access | ✅ | `stdin_open` and `tty` |
| Resource Limits | ✅ | CPU and memory constraints |
| Health Checks | ✅ | Supports test commands and HTTP checks |
| User Settings | ✅ | Numeric user/group IDs only |

### Networking

| Feature | Status | Description |
|---------|:------:|-------------|
| Ports | ✅ | TCP/UDP port mapping |
| Service Exposure | ✅ | Via Kubernetes annotations |
| Internal DNS | ❌ | Use Kubernetes DNS instead |
| Custom Networks | ❌ | Use Kubernetes networking |

### Storage & State

| Feature | Status | Description |
|---------|:------:|-------------|
| Named Volumes | ✅ | Converts to PersistentVolumeClaims |
| Bind Mounts | ✅ | Creates ConfigMaps for files |
| Host Paths | ✅ | Via `kubepose.volume.hostPath` label |
| Tmpfs | ✅ | Maps to emptyDir with Memory medium |
| Volume Labels | ✅ | Preserved in K8s resources |

### Configuration & Secrets

| Feature | Status | Description |
|---------|:------:|-------------|
| File-based Secrets | ✅ | Creates Kubernetes Secrets |
| Environment Secrets | ✅ | Creates Kubernetes Secrets |
| External Secrets | ✅ | References existing K8s Secrets |
| Labels | ✅ | Preserved in K8s resources |
| Annotations | ✅ | Preserved in K8s resources |
| Profiles | ✅ | For environment-specific configs |

## Unsupported Features

Some Docker Compose features are intentionally not supported as they either:
- Have no direct Kubernetes equivalent
- Are better handled by native Kubernetes features
- Fall outside kubepose's scope

Key unsupported features include:
- 🛠️ Build configuration (use `docker buildkit bake`)
- 🔗 Container linking (use Kubernetes Services)
- 🏗️ Dependencies (use Kubernetes primitives)
- 🔐 Privileged mode and capabilities
- 📝 Logging configuration

## Best Practices

1. **Use Profiles** for environment-specific configurations
2. **Leverage Labels** for better resource organization
3. **Keep Secrets External** when possible
4. **Use Standard Ports** to maintain compatibility

## Status Legend

| Symbol | Meaning |
|:------:|----------|
| ✅ | Fully Supported |
| 🚧 | Coming Soon |
| ❌ | Not Supported |


### Update Strategies

kubepose supports Docker Compose's `update_config` for controlling how services are updated:

```yaml
services:
  web:
    deploy:
      update_config:
        parallelism: 2     # How many containers to update at once
        order: start-first # Update strategy: start-first, stop-first
        delay: 10s        # Minimum time between updates
        monitor: 60s      # Time to monitor for failure
```

The configuration maps to Kubernetes deployment strategies as follows:

| Compose Config | Deployment | DaemonSet | Description |
|---------------|------------|-----------|-------------|
| `order: start-first` | RollingUpdate with `maxUnavailable: 0` | RollingUpdate with `maxSurge` | Start new pods before stopping old |
| `order: stop-first` | Recreate | RollingUpdate with `maxUnavailable` | Stop old pods before starting new |
| `parallelism` | `maxSurge`/`maxUnavailable` | `maxSurge`/`maxUnavailable` | Number of pods updated at once |
| `delay` | `minReadySeconds` | `minReadySeconds` | Time between updates |
| `monitor` | `progressDeadlineSeconds` | N/A | Time to monitor for failures |

Example configurations:

```yaml
# Rolling update that starts new pods first
services:
  web:
    deploy:
      update_config:
        order: start-first
        parallelism: 2

# Stop all pods before starting new ones
services:
  db:
    deploy:
      update_config:
        order: stop-first

# Gradual rollout with monitoring
services:
  api:
    deploy:
      update_config:
        parallelism: 1
        delay: 30s
        monitor: 60s
```

Note that some aspects of Docker Compose's update configuration don't have direct equivalents in Kubernetes:
- `failure_action` is handled differently through Kubernetes' native deployment controller
- `max_failure_ratio` has no direct equivalent

## Contributing

Contributions are welcome! See our [Contributing Guide](CONTRIBUTING.md) for details.

## License

[MIT License](LICENSE)
