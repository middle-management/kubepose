package kubepose

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	ServiceSelectorLabelKey                    = "kubepose.service"
	ServiceGroupLabelKey                       = "kubepose.service.group"
	ServiceAccountNameAnnotationKey            = "kubepose.service.serviceAccountName"
	ServiceIgnoreAnnotationKey                 = "kubepose.service.ignore"
	ServiceExposeAnnotationKey                 = "kubepose.service.expose"
	ServiceExposeIngressClassNameAnnotationKey = "kubepose.service.expose.ingress-class-name"
	HealthcheckHttpPathAnnotationKey           = "kubepose.healthcheck.http.path"
	ContainerTypeLabelKey                      = "kubepose.container.type"
)

func Convert(project *types.Project) (*Resources, error) {
	resources := &Resources{}

	secretMappings, err := processSecrets(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing secrets: %w", err)
	}

	configMappings, err := processConfigs(project, resources) // Add this line
	if err != nil {
		return nil, fmt.Errorf("error processing configs: %w", err)
	}

	volumeMappings, err := processVolumes(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing volumes: %w", err)
	}

	// Group services by kubepose.service.group
	groups := make(map[string][]types.ServiceConfig)
	for _, service := range project.Services {
		// Skip services with annotation
		if value, ok := service.Annotations[ServiceIgnoreAnnotationKey]; ok {
			ignored, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %s annotation: %w", ServiceIgnoreAnnotationKey, err)
			} else if ignored {
				continue
			}
		}

		// Handle standalone pods (non-Always restart policy)
		if getRestartPolicy(service) != corev1.RestartPolicyAlways && service.Labels[ContainerTypeLabelKey] != "init" {
			pod := createPod(service)
			pod.Spec.Containers = []corev1.Container{createContainer(service)}
			updatePodSpecWithSecrets(&pod.Spec, service, secretMappings)
			updatePodSpecWithConfigs(&pod.Spec, service, configMappings)
			updatePodSpecWithVolumes(&pod.Spec, service, volumeMappings, resources, project)
			resources.Pods = append(resources.Pods, pod)
			continue
		}

		groupName := service.Labels[ServiceGroupLabelKey]
		if groupName == "" {
			groupName = service.Name // Use service name as group if not specified
		}
		groups[groupName] = append(groups[groupName], service)
	}

	var groupNames []string
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	// Process groups in sorted order
	for _, groupName := range groupNames {
		services := groups[groupName]

		// Find main services (not init)
		var appServices, initServices []types.ServiceConfig
		for _, svc := range services {
			if svc.Labels[ContainerTypeLabelKey] == "init" {
				initServices = append(initServices, svc)
			} else {
				appServices = append(appServices, svc)
			}
		}

		if len(appServices) == 0 {
			continue
		}

		// Sort services by name for consistent ordering
		sort.Slice(appServices, func(i, j int) bool {
			return appServices[i].Name < appServices[j].Name
		})
		sort.Slice(initServices, func(i, j int) bool {
			return initServices[i].Name < initServices[j].Name
		})

		for _, service := range appServices {
			if service.Deploy != nil && service.Deploy.Mode == "global" {
				ds := createDaemonSet(resources, service)
				addContainersToSpec(&ds.Spec.Template.Spec, appServices, initServices)
				for _, svc := range append(appServices, initServices...) {
					updatePodSpecWithSecrets(&ds.Spec.Template.Spec, svc, secretMappings)
					updatePodSpecWithConfigs(&ds.Spec.Template.Spec, svc, configMappings)
					updatePodSpecWithVolumes(&ds.Spec.Template.Spec, svc, volumeMappings, resources, project)
				}
				removeDuplicateVolumeMounts(ds.Spec.Template.Spec.Containers)
				removeDuplicateVolumeMounts(ds.Spec.Template.Spec.InitContainers)
			} else {
				deploy := createDeployment(resources, service)
				addContainersToSpec(&deploy.Spec.Template.Spec, appServices, initServices)
				for _, svc := range append(appServices, initServices...) {
					updatePodSpecWithSecrets(&deploy.Spec.Template.Spec, svc, secretMappings)
					updatePodSpecWithConfigs(&deploy.Spec.Template.Spec, svc, configMappings)
					updatePodSpecWithVolumes(&deploy.Spec.Template.Spec, svc, volumeMappings, resources, project)
				}
				removeDuplicateVolumeMounts(deploy.Spec.Template.Spec.Containers)
				removeDuplicateVolumeMounts(deploy.Spec.Template.Spec.InitContainers)
			}

			if len(service.Ports) > 0 {
				svc := createService(service)
				found := false
				for _, s := range resources.Services {
					if s.ObjectMeta.Name == svc.ObjectMeta.Name {
						found = true
						break
					}
				}
				if !found {
					resources.Services = append(resources.Services, svc)
					if _, ok := service.Annotations[ServiceExposeAnnotationKey]; ok {
						resources.Ingresses = append(resources.Ingresses, createIngress(service))
					}
				}
			}
		}
	}

	return resources, nil
}

func addContainersToSpec(podSpec *corev1.PodSpec, appServices, initServices []types.ServiceConfig) {
nextInitService:
	for _, svc := range initServices {
		for _, container := range podSpec.InitContainers {
			if container.Name == svc.Name {
				continue nextInitService
			}
		}
		podSpec.InitContainers = append(podSpec.InitContainers, createContainer(svc))
	}
nextService:
	for _, svc := range appServices {
		for _, container := range podSpec.Containers {
			if container.Name == svc.Name {
				continue nextService
			}
		}
		podSpec.Containers = append(podSpec.Containers, createContainer(svc))
	}
}

func createContainer(service types.ServiceConfig) corev1.Container {
	livenessProbe, readinessProbe := getProbes(service)

	// support for init containers with always restart policy
	// (also known as side car containers)
	// https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/
	var containerRestartPolicy *corev1.ContainerRestartPolicy
	if service.Labels[ContainerTypeLabelKey] == "init" && getRestartPolicy(service) == corev1.RestartPolicyAlways {
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
		RestartPolicy:   containerRestartPolicy,
	}
}

func createPod(service types.ServiceConfig) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      getRestartPolicy(service),
			ServiceAccountName: service.Annotations[ServiceAccountNameAnnotationKey],
		},
	}
}

func createDaemonSet(resources *Resources, service types.ServiceConfig) *appsv1.DaemonSet {
	serviceName := service.Name
	if service.Labels[ServiceGroupLabelKey] != "" {
		serviceName = service.Labels[ServiceGroupLabelKey]
	}

	for _, ds := range resources.DaemonSets {
		if ds.ObjectMeta.Name == serviceName {
			return ds
		}
	}

	ds := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: getUpdateStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ServiceSelectorLabelKey: service.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: service.Annotations,
					Labels: mergeMaps(service.Labels, map[string]string{
						ServiceSelectorLabelKey: service.Name,
					}),
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      getRestartPolicy(service),
					ServiceAccountName: service.Annotations[ServiceAccountNameAnnotationKey],
				},
			},
		},
	}
	resources.DaemonSets = append(resources.DaemonSets, ds)
	return ds
}

func createDeployment(resources *Resources, service types.ServiceConfig) *appsv1.Deployment {
	serviceName := service.Name
	if service.Labels[ServiceGroupLabelKey] != "" {
		serviceName = service.Labels[ServiceGroupLabelKey]
	}

	for _, d := range resources.Deployments {
		if d.ObjectMeta.Name == serviceName {
			return d
		}
	}

	var replicas *int32
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = ptr.To(int32(*service.Deploy.Replicas))
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Strategy: getDeploymentStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ServiceSelectorLabelKey: serviceName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: service.Annotations,
					Labels: mergeMaps(service.Labels, map[string]string{
						ServiceSelectorLabelKey: serviceName,
					}),
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      getRestartPolicy(service),
					ServiceAccountName: service.Annotations[ServiceAccountNameAnnotationKey],
				},
			},
		},
	}
	resources.Deployments = append(resources.Deployments, d)
	return d
}

func mergeMaps(maps ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}

var reEnvVars = regexp.MustCompile(`\$([a-zA-Z0-9.-_]+)`)

func escapeEnvs(input []string) []string {
	var args []string
	for _, arg := range input {
		args = append(args, reEnvVars.ReplaceAllString(arg, `$($1)`))
	}
	return args
}

func createService(service types.ServiceConfig) *corev1.Service {
	serviceName := service.Name
	if service.Labels[ServiceGroupLabelKey] != "" {
		serviceName = service.Labels[ServiceGroupLabelKey]
	}
	// TODO support LoadBalancer, NodePort, ExternalName, ClusterIP
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				ServiceSelectorLabelKey: serviceName,
			},
			Ports: convertServicePorts(service.Ports),
		},
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

func convertServicePorts(ports []types.ServicePortConfig) []corev1.ServicePort {
	var servicePorts []corev1.ServicePort
	for _, port := range ports {
		published := int(port.Target)
		if port.Published != "" {
			published, _ = strconv.Atoi(port.Published)
		}
		servicePort := corev1.ServicePort{
			Name:       strconv.Itoa(published),
			Port:       int32(published),
			TargetPort: intstr.FromInt(int(port.Target)),
			Protocol:   convertProtocol(port.Protocol),
		}
		servicePorts = append(servicePorts, servicePort)
	}
	return servicePorts
}

func convertProtocol(protocol string) corev1.Protocol {
	switch strings.ToUpper(protocol) {
	case "TCP":
		return corev1.ProtocolTCP
	case "UDP":
		return corev1.ProtocolUDP
	default:
		return corev1.ProtocolTCP
	}
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
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Limits.NanoCPUs.Value())/1e6)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Limits.MemoryBytes/1024/1024)),
			}
		}
		if service.Deploy.Resources.Reservations != nil {
			resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", int(service.Deploy.Resources.Reservations.NanoCPUs.Value())/1e6)),
				corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", service.Deploy.Resources.Reservations.MemoryBytes/1024/1024)),
			}
		}
	}

	return resources
}

func createIngress(service types.ServiceConfig) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	var ingressClassName *string

	// Check if a specific ingress class is specified in annotations
	if class, ok := service.Annotations[ServiceExposeIngressClassNameAnnotationKey]; ok {
		ingressClassName = &class
	}

	// Get host from labels or annotations
	host := service.Name // Default host
	if h, ok := service.Annotations[ServiceExposeAnnotationKey]; ok && h != "true" {
		host = h
	}

	// Find the first HTTP port
	var servicePort int32
	for _, port := range service.Ports {
		if port.Protocol == "" || strings.ToUpper(port.Protocol) == "TCP" {
			published := int32(port.Target)
			if port.Published != "" {
				if p, err := strconv.Atoi(port.Published); err == nil {
					published = int32(p)
				}
			}
			servicePort = published
			break
		}
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: service.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: servicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func getRestartPolicy(service types.ServiceConfig) corev1.RestartPolicy {
	if service.Deploy != nil && service.Deploy.RestartPolicy != nil {
		switch service.Deploy.RestartPolicy.Condition {
		case "on-failure":
			return corev1.RestartPolicyOnFailure
		case "never":
			return corev1.RestartPolicyNever
		}
	}

	// TODO restart: on-failure[:max-retries] should probably fail...

	switch strings.ToLower(service.Restart) {
	case "always":
		return corev1.RestartPolicyAlways
	case "no":
		return corev1.RestartPolicyNever
	case "unless-stopped", "on-failure":
		return corev1.RestartPolicyOnFailure
	}

	if service.Labels[ContainerTypeLabelKey] == "init" {
		// init containers default to on-failure policy
		return corev1.RestartPolicyOnFailure
	}

	// compose default is "no" but that is not valid in k8s deployments etc
	return corev1.RestartPolicyAlways
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

func getProbes(service types.ServiceConfig) (liveness *corev1.Probe, readiness *corev1.Probe) {
	if service.HealthCheck == nil || service.HealthCheck.Disable {
		return nil, nil
	}

	var probe *corev1.Probe

	// Convert test command
	if len(service.HealthCheck.Test) > 0 {
		// Handle different formats of test
		var command []string
		switch service.HealthCheck.Test[0] {
		case "CMD", "CMD-SHELL":
			command = service.HealthCheck.Test[1:]
		default:
			command = service.HealthCheck.Test
		}

		probe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: command,
				},
			},
		}
	}

	if probe != nil {
		// Convert timing parameters
		if service.HealthCheck.Interval != nil {
			probe.PeriodSeconds = int32(time.Duration(*service.HealthCheck.Interval).Seconds())
		}
		if service.HealthCheck.Timeout != nil {
			probe.TimeoutSeconds = int32(time.Duration(*service.HealthCheck.Timeout).Seconds())
		}
		if service.HealthCheck.StartPeriod != nil {
			probe.InitialDelaySeconds = int32(time.Duration(*service.HealthCheck.StartPeriod).Seconds())
		}
		if service.HealthCheck.Retries != nil {
			probe.FailureThreshold = int32(*service.HealthCheck.Retries)
		}

		// Use the same probe for both liveness and readiness
		liveness = probe.DeepCopy()
		readiness = probe.DeepCopy()
	}

	// Check for HTTP-specific health check annotations
	if path, ok := service.Annotations[HealthcheckHttpPathAnnotationKey]; ok {
		httpProbe := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: path,
					Port: intstr.FromInt(getFirstPort(service)),
				},
			},
		}

		// Copy timing parameters if they exist
		if probe != nil {
			httpProbe.PeriodSeconds = probe.PeriodSeconds
			httpProbe.TimeoutSeconds = probe.TimeoutSeconds
			httpProbe.InitialDelaySeconds = probe.InitialDelaySeconds
			httpProbe.FailureThreshold = probe.FailureThreshold
		}

		liveness = httpProbe
		readiness = httpProbe.DeepCopy()
	}

	return liveness, readiness
}

func getFirstPort(service types.ServiceConfig) int {
	if len(service.Ports) > 0 {
		if published, err := strconv.Atoi(service.Ports[0].Published); err == nil {
			return published
		}
		return int(service.Ports[0].Target)
	}
	return 80 // default port if none specified
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

func getDeploymentStrategy(service types.ServiceConfig) appsv1.DeploymentStrategy {
	if service.Deploy != nil && service.Deploy.UpdateConfig != nil {
		updateConfig := service.Deploy.UpdateConfig

		var maxSurge *intstr.IntOrString
		if updateConfig.Parallelism != nil {
			maxSurge = &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(ptr.Deref(updateConfig.Parallelism, 0)),
			}
		}

		switch updateConfig.Order {
		case "stop-first":
			return appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			}
		case "start-first":
			return appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: maxSurge,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			}
		default: // default to RollingUpdate with some unavailability allowed
			return appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: maxSurge,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
		}
	}

	return appsv1.DeploymentStrategy{}
}

func getUpdateStrategy(service types.ServiceConfig) appsv1.DaemonSetUpdateStrategy {
	if service.Deploy != nil && service.Deploy.UpdateConfig != nil {
		updateConfig := service.Deploy.UpdateConfig

		var parallelism *intstr.IntOrString
		if updateConfig.Parallelism != nil {
			parallelism = &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(ptr.Deref(updateConfig.Parallelism, 0)),
			}
		}

		switch updateConfig.Order {
		case "start-first":
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: parallelism,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			}
		case "stop-first":
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxUnavailable: parallelism,
				},
			}
		default:
			// Default to allowing both surge and unavailability
			return appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge: parallelism,
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
		}
	}

	return appsv1.DaemonSetUpdateStrategy{}
}
