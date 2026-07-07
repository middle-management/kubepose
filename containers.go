package kubepose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func (t Transformer) createContainer(service types.ServiceConfig) corev1.Container {
	livenessProbe, readinessProbe, startupProbe := getProbes(service)

	// An init-typed service becomes a native sidecar: an init container with
	// restartPolicy Always. validateService guarantees restart: always here.
	// https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/
	var containerRestartPolicy *corev1.ContainerRestartPolicy
	if service.Annotations[ContainerTypeAnnotationKey] == "init" {
		containerRestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
	}
	return corev1.Container{
		Name:            service.Name,
		Image:           service.Image,
		Command:         service.Entrypoint,
		WorkingDir:      service.WorkingDir,
		Stdin:           service.StdinOpen,
		TTY:             service.Tty,
		Args:            escapeEnvs(service.Command),
		Ports:           convertPorts(service.Ports),
		Env:             convertEnvironment(service.Environment),
		Resources:       getResourceRequirements(service),
		ImagePullPolicy: getImagePullPolicy(service),
		LivenessProbe:   livenessProbe,
		ReadinessProbe:  readinessProbe,
		StartupProbe:    startupProbe,
		RestartPolicy:   containerRestartPolicy,
	}
}

func preStartContainerName(service types.ServiceConfig, index int) string {
	return fmt.Sprintf("%s-pre-start-%d", service.Name, index)
}

// createPreStartContainers converts a service's pre_start lifecycle hooks into
// init containers. Kubernetes runs init containers sequentially and each must
// exit 0 before the next starts, matching the compose pre_start contract.
func (t Transformer) createPreStartContainers(service types.ServiceConfig) []corev1.Container {
	var containers []corev1.Container
	for i, hook := range service.PreStart {
		image := hook.Image
		if image == "" {
			image = service.Image
		}

		env := make(types.MappingWithEquals, len(service.Environment)+len(hook.Environment))
		for k, v := range service.Environment {
			env[k] = v
		}
		for k, v := range hook.Environment {
			env[k] = v
		}

		containers = append(containers, corev1.Container{
			Name:            preStartContainerName(service, i),
			Image:           image,
			Command:         escapeEnvs(hook.Command),
			WorkingDir:      hook.WorkingDir,
			Env:             convertEnvironment(env),
			ImagePullPolicy: getImagePullPolicy(service),
			SecurityContext: getHookSecurityContext(hook),
		})
	}
	return containers
}

// inheritPreStartVolumeMounts copies the parent service container's volume
// mounts onto its pre_start init containers. The secret/config/volume plumbing
// attaches mounts by matching container name to service name, which never
// matches hook containers, so this must run after those updates.
func inheritPreStartVolumeMounts(spec *corev1.PodSpec, service types.ServiceConfig) {
	if len(service.PreStart) == 0 {
		return
	}

	var parentMounts []corev1.VolumeMount
	for _, container := range spec.Containers {
		if container.Name == service.Name {
			parentMounts = container.VolumeMounts
			break
		}
	}
	if parentMounts == nil {
		for _, container := range spec.InitContainers {
			if container.Name == service.Name {
				parentMounts = container.VolumeMounts
				break
			}
		}
	}
	if len(parentMounts) == 0 {
		return
	}

	for i := range spec.InitContainers {
		for hi := range service.PreStart {
			if spec.InitContainers[i].Name == preStartContainerName(service, hi) {
				spec.InitContainers[i].VolumeMounts = append(
					spec.InitContainers[i].VolumeMounts,
					parentMounts...,
				)
			}
		}
	}
}

func convertPorts(ports []types.ServicePortConfig) []corev1.ContainerPort {
	var containerPorts []corev1.ContainerPort
	for _, port := range ports {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			ContainerPort: int32(port.Target),
			Protocol:      convertProtocol(port.Protocol),
		})
	}
	return containerPorts
}

func convertEnvironment(env map[string]*string) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	for key, value := range env {
		envVar := corev1.EnvVar{
			Name: key,
		}
		if value != nil {
			envVar.Value = *value
		}
		envVars = append(envVars, envVar)
	}
	sort.Slice(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

func getResourceRequirements(service types.ServiceConfig) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}

	if service.Deploy != nil {
		if service.Deploy.Resources.Limits != nil {
			resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Limits.NanoCPUs.Value()*1000))),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Limits.MemoryBytes/1024/1024)),
			}
		}
		if service.Deploy.Resources.Reservations != nil {
			resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Reservations.NanoCPUs.Value()*1000))),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Reservations.MemoryBytes/1024/1024)),
			}
		}
	}

	return resources
}

func getImagePullPolicy(service types.ServiceConfig) corev1.PullPolicy {
	if service.PullPolicy == "" {
		return corev1.PullIfNotPresent // default behavior
	}

	switch strings.ToLower(service.PullPolicy) {
	case "always":
		return corev1.PullAlways
	case "never":
		return corev1.PullNever
	case "if_not_present", "missing":
		return corev1.PullIfNotPresent
	default:
		return corev1.PullIfNotPresent
	}
}

func removeDuplicateVolumeMounts(containers []corev1.Container) {
	for i := range containers {
		seen := make(map[string]bool)
		var unique []corev1.VolumeMount
		for _, mount := range containers[i].VolumeMounts {
			key := mount.Name + ":" + mount.MountPath
			if !seen[key] {
				seen[key] = true
				unique = append(unique, mount)
			}
		}
		containers[i].VolumeMounts = unique
	}
}
