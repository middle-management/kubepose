package kubepose

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	AppSelectorLabelKey                        = "app.kubernetes.io/name"
	ServiceGroupAnnotationKey                  = "kubepose.service.group"
	ServiceAccountNameAnnotationKey            = "kubepose.service.serviceAccountName"
	ServiceExposeAnnotationKey                 = "kubepose.service.expose"
	ServiceExposeIngressClassNameAnnotationKey = "kubepose.service.expose.ingressClassName"
	SelectorMatchLabelsAnnotationKey           = "kubepose.selector.matchLabels"
	HealthcheckHttpGetPathAnnotationKey        = "kubepose.healthcheck.httpGet.path"
	HealthcheckHttpGetPortAnnotationKey        = "kubepose.healthcheck.httpGet.port"
	ContainerTypeAnnotationKey                 = "kubepose.container.type"
	ConfigHmacKeyAnnotationKey                 = "kubepose.config.hmacKey"
	SecretHmacKeyAnnotationKey                 = "kubepose.secret.hmacKey"
	VolumeHmacKeyAnnotationKey                 = "kubepose.volume.hmacKey"
	VolumeHostPathLabelKey                     = "kubepose.volume.hostPath"
	VolumeStorageClassNameLabelKey             = "kubepose.volume.storageClassName"
	VolumeSizeLabelKey                         = "kubepose.volume.size"
	SecretSubPathLabelKey                      = "kubepose.secret.subPath"

	// using a hmac key to be able to invalidate if we modify how an immutable volume/config/secret is shaped
	volumeHmacKey    = "kubepose.volume.v1"
	configHmacKey    = "kubepose.config.v1"
	secretHmacKey    = "kubepose.secret.v1"
	configDefaultKey = "content"
)

type Transformer struct {
	Annotations map[string]string
	Labels      map[string]string
}

func (t Transformer) Convert(project *types.Project) (*Resources, error) {
	resources := &Resources{}

	secretMappings, err := t.processSecrets(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing secrets: %w", err)
	}

	configMappings, err := t.processConfigs(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing configs: %w", err)
	}

	volumeMappings, err := t.processVolumes(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing volumes: %w", err)
	}

	// Create a map to track created service accounts to avoid duplicates
	createdServiceAccounts := make(map[string]bool)

	// Process service accounts first
	for _, service := range project.Services {
		if saName, ok := service.Annotations[ServiceAccountNameAnnotationKey]; ok && saName != "" {
			if !createdServiceAccounts[saName] {
				resources.ServiceAccounts = append(resources.ServiceAccounts,
					t.createServiceAccount(saName, service))
				createdServiceAccounts[saName] = true
			}
		}
	}

	// Group services by kubepose.service.group
	groups := make(map[string][]types.ServiceConfig)
	for _, service := range project.Services {
		// Handle standalone pods (non-Always restart policy)
		if getRestartPolicy(service) != corev1.RestartPolicyAlways && service.Annotations[ContainerTypeAnnotationKey] != "init" {
			pod := t.createPod(service)
			pod.Spec.Containers = []corev1.Container{t.createContainer(service)}
			t.updatePodSpecWithSecrets(&pod.Spec, service, secretMappings)
			t.updatePodSpecWithConfigs(&pod.Spec, service, configMappings)
			t.updatePodSpecWithVolumes(&pod.Spec, service, volumeMappings, resources)
			resources.Pods = append(resources.Pods, pod)
			continue
		}

		groupName := service.Annotations[ServiceGroupAnnotationKey]
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
			if svc.Annotations[ContainerTypeAnnotationKey] == "init" {
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
				ds := t.createDaemonSet(resources, service)
				t.addContainersToSpec(&ds.Spec.Template.Spec, appServices, initServices)
				for _, svc := range append(appServices, initServices...) {
					t.updatePodSpecWithSecrets(&ds.Spec.Template.Spec, svc, secretMappings)
					t.updatePodSpecWithConfigs(&ds.Spec.Template.Spec, svc, configMappings)
					t.updatePodSpecWithVolumes(&ds.Spec.Template.Spec, svc, volumeMappings, resources)
				}
				removeDuplicateVolumeMounts(ds.Spec.Template.Spec.Containers)
				removeDuplicateVolumeMounts(ds.Spec.Template.Spec.InitContainers)
			} else {
				deploy := t.createDeployment(resources, service)
				t.addContainersToSpec(&deploy.Spec.Template.Spec, appServices, initServices)
				for _, svc := range append(appServices, initServices...) {
					t.updatePodSpecWithSecrets(&deploy.Spec.Template.Spec, svc, secretMappings)
					t.updatePodSpecWithConfigs(&deploy.Spec.Template.Spec, svc, configMappings)
					t.updatePodSpecWithVolumes(&deploy.Spec.Template.Spec, svc, volumeMappings, resources)
				}
				removeDuplicateVolumeMounts(deploy.Spec.Template.Spec.Containers)
				removeDuplicateVolumeMounts(deploy.Spec.Template.Spec.InitContainers)
			}

			if len(service.Ports) > 0 {
				svc := t.createService(service)
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
						resources.Ingresses = append(resources.Ingresses, t.createIngress(service))
					}
				}
			}
		}
	}

	return resources, nil
}

func (t Transformer) addContainersToSpec(podSpec *corev1.PodSpec, appServices, initServices []types.ServiceConfig) {
nextInitService:
	for _, svc := range initServices {
		for _, container := range podSpec.InitContainers {
			if container.Name == svc.Name {
				continue nextInitService
			}
		}
		podSpec.InitContainers = append(podSpec.InitContainers, t.createContainer(svc))
	}
nextService:
	for _, svc := range appServices {
		for _, container := range podSpec.Containers {
			if container.Name == svc.Name {
				continue nextService
			}
		}
		podSpec.Containers = append(podSpec.Containers, t.createContainer(svc))
	}
}

func (t Transformer) createContainer(service types.ServiceConfig) corev1.Container {
	livenessProbe, readinessProbe := getProbes(service)

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
		Ports:           t.convertPorts(service.Ports),
		Env:             convertEnvironment(service.Environment),
		Resources:       t.getResourceRequirements(service),
		ImagePullPolicy: t.getImagePullPolicy(service),
		LivenessProbe:   livenessProbe,
		ReadinessProbe:  readinessProbe,
		RestartPolicy:   containerRestartPolicy,
	}
}

func (t Transformer) createPod(service types.ServiceConfig) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: t.createPodSpec(service),
	}
}

func (t Transformer) createDaemonSet(resources *Resources, service types.ServiceConfig) *appsv1.DaemonSet {
	serviceName := service.Name
	if service.Annotations[ServiceGroupAnnotationKey] != "" {
		serviceName = service.Annotations[ServiceGroupAnnotationKey]
	}

	for _, ds := range resources.DaemonSets {
		if ds.ObjectMeta.Name == serviceName {
			return ds
		}
	}

	matchLabels := map[string]string{
		AppSelectorLabelKey: serviceName,
	}
	if annotation, ok := service.Annotations[SelectorMatchLabelsAnnotationKey]; ok {
		newMatchLabels := make(map[string]string)
		err := json.Unmarshal([]byte(annotation), &newMatchLabels)
		if err != nil {
			logrus.Warnf("Error parsing selector match labels: %v\n", err)
		} else {
			matchLabels = newMatchLabels
		}
	}

	ds := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: getUpdateStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: mergeMaps(service.Annotations, t.Annotations),
					Labels: mergeMaps(service.Labels, map[string]string{
						AppSelectorLabelKey: serviceName,
					}),
				},
				Spec: t.createPodSpec(service),
			},
		},
	}
	resources.DaemonSets = append(resources.DaemonSets, ds)
	return ds
}

func (t Transformer) createDeployment(resources *Resources, service types.ServiceConfig) *appsv1.Deployment {
	serviceName := service.Name
	if service.Annotations[ServiceGroupAnnotationKey] != "" {
		serviceName = service.Annotations[ServiceGroupAnnotationKey]
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

	matchLabels := map[string]string{
		AppSelectorLabelKey: serviceName,
	}
	if annotation, ok := service.Annotations[SelectorMatchLabelsAnnotationKey]; ok {
		newMatchLabels := make(map[string]string)
		err := json.Unmarshal([]byte(annotation), &newMatchLabels)
		if err != nil {
			logrus.Warnf("Error parsing selector match labels: %v\n", err)
		} else {
			matchLabels = newMatchLabels
		}
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Strategy: getDeploymentStrategy(service),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: mergeMaps(service.Annotations, t.Annotations),
					Labels: mergeMaps(service.Labels, map[string]string{
						AppSelectorLabelKey: serviceName,
					}),
				},
				Spec: t.createPodSpec(service),
			},
		},
	}
	resources.Deployments = append(resources.Deployments, d)
	return d
}

func (t Transformer) createPodSpec(service types.ServiceConfig) corev1.PodSpec {
	return corev1.PodSpec{
		RestartPolicy:      getRestartPolicy(service),
		SecurityContext:    getSecurityContext(service),
		ServiceAccountName: service.Annotations[ServiceAccountNameAnnotationKey],
	}
}

func (t Transformer) createServiceAccount(name string, service types.ServiceConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      service.Labels,
		},
	}
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

func (t Transformer) createService(service types.ServiceConfig) *corev1.Service {
	serviceName := service.Name
	if service.Annotations[ServiceGroupAnnotationKey] != "" {
		serviceName = service.Annotations[ServiceGroupAnnotationKey]
	}
	// TODO support LoadBalancer, NodePort, ExternalName, ClusterIP
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				AppSelectorLabelKey: serviceName,
			},
			Ports: t.convertServicePorts(service.Ports),
		},
	}
}

func (t Transformer) convertPorts(ports []types.ServicePortConfig) []corev1.ContainerPort {
	var containerPorts []corev1.ContainerPort
	for _, port := range ports {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			ContainerPort: int32(port.Target),
			Protocol:      convertProtocol(port.Protocol),
		})
	}
	return containerPorts
}

func (t Transformer) convertServicePorts(ports []types.ServicePortConfig) []corev1.ServicePort {
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

func (t Transformer) getResourceRequirements(service types.ServiceConfig) corev1.ResourceRequirements {
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

func (t Transformer) createIngress(service types.ServiceConfig) *networkingv1.Ingress {
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
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
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

	if service.Annotations[ContainerTypeAnnotationKey] == "init" {
		// init containers default to on-failure policy
		return corev1.RestartPolicyOnFailure
	}

	// compose default is "no" but that is not valid in k8s deployments etc
	return corev1.RestartPolicyAlways
}

func (t Transformer) getImagePullPolicy(service types.ServiceConfig) corev1.PullPolicy {
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
	if service.HealthCheck != nil && service.HealthCheck.Disable {
		return nil, nil
	}

	var probe *corev1.Probe

	// Convert test command
	if service.HealthCheck != nil && len(service.HealthCheck.Test) > 0 {
		var command []string
		switch service.HealthCheck.Test[0] {
		case "CMD", "CMD-SHELL":
			command = service.HealthCheck.Test[1:]
		default:
			command = service.HealthCheck.Test
		}
		if len(command) == 1 {
			command = splitCommand(command[0])
		}

		probe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: command,
				},
			},
		}
	}

	if probe != nil && service.HealthCheck != nil {
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
	if path, ok := service.Annotations[HealthcheckHttpGetPathAnnotationKey]; ok {
		httpGetPort := getFirstPort(service)
		if port, ok := service.Annotations[HealthcheckHttpGetPortAnnotationKey]; ok {
			if p, err := strconv.Atoi(port); err == nil {
				httpGetPort = p
			}
		}

		httpProbe := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: path,
					Port: intstr.FromInt(httpGetPort),
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

	// TODO TCP and GRPC health checks

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

func getSecurityContext(service types.ServiceConfig) *corev1.PodSecurityContext {
	var runAsUser, runAsGroup, fsGroup *int64
	var supplementalGroups []int64

	if service.User != "" {
		parts := strings.Split(service.User, ":")

		if uid, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			runAsUser = &uid
		} else {
			fmt.Printf("Warning: skipping named user %q - only numeric IDs are supported\n", parts[0])
		}

		if len(parts) > 1 {
			if gid, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				runAsGroup = &gid
				fsGroup = &gid
			} else {
				fmt.Printf("Warning: skipping named group %q - only numeric IDs are supported\n", parts[1])
			}
		}
	}

	for _, g := range service.GroupAdd {
		if gid, err := strconv.ParseInt(g, 10, 64); err == nil {
			supplementalGroups = append(supplementalGroups, gid)
		} else {
			fmt.Printf("Warning: skipping named group %q - only numeric IDs are supported\n", g)
		}
	}

	if runAsUser == nil && runAsGroup == nil && fsGroup == nil && len(supplementalGroups) == 0 {
		return nil
	}

	return &corev1.PodSecurityContext{
		RunAsUser:          runAsUser,
		RunAsGroup:         runAsGroup,
		FSGroup:            fsGroup,
		SupplementalGroups: supplementalGroups,
	}
}

func splitCommand(cmd string) []string {
	var args []string
	var current string
	var inQuotes bool

	for _, r := range cmd {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case ' ':
			if !inQuotes {
				if current != "" {
					args = append(args, current)
					current = ""
				}
			} else {
				current += string(r)
			}
		default:
			current += string(r)
		}
	}

	if current != "" {
		args = append(args, current)
	}

	return args
}
