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

	// support for init containers with always restart policy
	// (also known as side car containers)
	// https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/
	var containerRestartPolicy *corev1.ContainerRestartPolicy
	if service.Annotations[ContainerTypeAnnotationKey] == "init" && getRestartPolicy(service) == corev1.RestartPolicyAlways {
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
