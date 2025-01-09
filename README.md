# kubepose

A simpler version of kompose, which only converts compose spec to k8s yaml files.

Unlike kompose it will:
- not build or push images (use `docker buildkit bake` for that)
- not set a namespace (use `kubectl apply -n <ns>` instead)
- not use mutable secrets and configmaps

| Compose Feature | Support Status | Notes |
|----------------|----------------|-------|
| **Services - Basic Configuration** |
| image | ✅ Supported | |
| container_name | ❌ Not Supported | K8s generates pod names |
| command | ✅ Supported | |
| entrypoint | ✅ Supported | |
| environment | ✅ Supported | |
| env_file | ❌ Not Supported | |
| working_dir | ✅ Supported | |
| user | ⏲️ Not Supported Yet | |
| stdin_open | ✅ Supported | |
| tty | ✅ Supported | |
| **Services - Networking** |
| ports | ✅ Supported | TCP/UDP protocols |
| expose | ✅ Supported | Via annotations |
| networks | ❌ Not Supported | |
| links | ❌ Not Supported | Legacy feature |
| dns | ❌ Not Supported | |
| dns_search | ❌ Not Supported | |
| extra_hosts | ❌ Not Supported | |
| network_mode | ❌ Not Supported | |
| **Services - Dependencies** |
| depends_on | ❌ Not Supported | No equivalent in Kubernetes |
| **Services - Health Checks** |
| healthcheck | ⏲️ Not Supported Yet | |
| **Deploy** |
| mode | ✅ Supported | `replicated` and `global` modes |
| replicas | ✅ Supported | |
| restart_policy | ✅ Supported | `always`, the default, will create a Workload, while `on-failure` or `never` will only create a Pod |
| resources.limits | ✅ Supported | CPU and Memory |
| resources.reservations | ✅ Supported | CPU and Memory |
| placement | ❌ Not Supported | |
| update_config | ❌ Not Supported | |
| rollback_config | ❌ Not Supported | |
| **Volumes** |
| named volumes | ✅ Supported | Creates PVC |
| bind mounts | ✅ Supported | Creates ConfigMap for files |
| tmpfs | ⏲️ Not Supported Yet | |
| volumes_from | ❌ Not Supported | |
| volume labels | ✅ Supported | |
| host path volumes | ✅ Supported | Via `x-kubepose-volume` extension or label |
| **Secrets** |
| file | ✅ Supported | Creates K8s Secret |
| environment | ✅ Supported | Creates K8s Secret |
| external | ✅ Supported | References existing K8s Secret |
| **Configs** |
| configs | ❌ Not Supported | |
| **Build** |
| build | ❌ Not Supported | |
| image | ✅ Supported | |
| args | ❌ Not Supported | |
| dockerfile | ❌ Not Supported | |
| context | ❌ Not Supported | |
| **Resource Constraints** |
| cpu_count | ❌ Not Supported | Use deploy.resources |
| cpu_percent | ❌ Not Supported | Use deploy.resources |
| mem_limit | ❌ Not Supported | Use deploy.resources |
| mem_reservation | ❌ Not Supported | Use deploy.resources |
| **Security** |
| cap_add | ❌ Not Supported | |
| cap_drop | ❌ Not Supported | |
| privileged | ❌ Not Supported | |
| security_opt | ❌ Not Supported | |
| **Logging** |
| logging | ❌ Not Supported | |
| **Miscellaneous** |
| labels | ✅ Supported | |
| annotations | ✅ Supported | |
| extends | ✅ Supported | |
| profiles | ✅ Supported | |
| init | ❌ Not Supported | |
| sysctls | ❌ Not Supported | |
| ulimits | ❌ Not Supported | |
| devices | ❌ Not Supported | |
| **Container Lifecycle** |
| stop_grace_period | ❌ Not Supported | |
| stop_signal | ❌ Not Supported | |
| **Runtime** |
| pid | ❌ Not Supported | |
| ipc | ❌ Not Supported | |
| shm_size | ❌ Not Supported | |
| **Storage** |
| storage_opt | ❌ Not Supported | |
| **Workload Types** |
| Deployments | ✅ Supported | Default for services |
| DaemonSets | ✅ Supported | Via deploy.mode: global |
| StatefulSets | ⏲️ Not Supported Yet | |
| CronJobs | ⏲️ Not Supported Yet | |
