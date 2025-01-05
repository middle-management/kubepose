# kubepose

A simpler version of kompose, which only converts compose spec to k8s yaml files.

Unlike kompose it will not:
- build or push images (use `docker buildkit bake` for that)
- set a namespace (use `kubectl apply -n <ns>` instead)
- support non-kubernetes
