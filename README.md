
# kubepose

> A minimalist tool to convert [Compose specification](https://compose-spec.io/) files to Kubernetes manifests

## Why kubepose?

kubepose provides a streamlined alternative to [kompose](https://kompose.io/), focusing solely on converting Compose specifications to Kubernetes YAML files with:

- ✨ **Zero Configuration** Your compose file is the only input needed
- 🎯 **Predictable Output** Generates clean, standard Kubernetes manifests
- 🔒 **Immutable by Default** Secrets and configmaps are created immutably

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

## Key Features

- 🎮 **Simple CLI** - Single command with familiar `-f` and `-p` flags
- 🔄 **Standard Conversion** - Predictable mapping to Kubernetes resources
- 📦 **No Dependencies** - Single binary with zero runtime requirements
- 🎯 **Targeted Scope** - Focused purely on Compose to Kubernetes conversion

## Supported Resources

### Core Workloads

| Feature | Status | Description |
|---------|:------:|-------------|
| Deployments | ✅ | Default workload type |
| DaemonSets | ✅ | Enable with `deploy.mode: global` |
| StatefulSets | 🚧 | Planned |
| CronJobs | 🚧 | Planned |

### Container Configuration

| Feature | Status | Description |
|---------|:------:|-------------|
| Image & Tags | ✅ | Full support for image references |
| Commands | ✅ | Both `command` and `entrypoint` |
| Environment | ✅ | Variables and values |
| Working Directory | ✅ | Via `working_dir` |
| Shell Access | ✅ | `stdin_open` and `tty` |
| Resource Limits | ✅ | CPU and memory constraints |
| Health Checks | 🚧 | Planned |
| User Settings | 🚧 | Planned |

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
| Host Paths | ✅ | Via `x-kubepose-volume` extension |
| Volume Labels | ✅ | Preserved in K8s resources |
| Tmpfs | 🚧 | Planned |

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

## Contributing

Contributions are welcome! See our [Contributing Guide](CONTRIBUTING.md) for details.

## License

[MIT License](LICENSE)
