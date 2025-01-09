package kubepose

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func Convert(project *types.Project) (*Resources, error) {
	// Initialize K8s resources
	resources := &Resources{}

	// Process secrets first
	secretMappings, err := processSecrets(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing secrets: %w", err)
	}

	// Process volumes
	volumeMappings, err := processVolumes(project, resources)
	if err != nil {
		return nil, fmt.Errorf("error processing volumes: %w", err)
	}

	// Convert each service to Kubernetes resources
	for _, service := range project.Services {
		// TODO StatefulSet, CronJob

		restartPolicy := getRestartPolicy(service)
		if restartPolicy != corev1.RestartPolicyAlways {
			// create pod for on-failure/never restart policies
			pod := createPod(service)

			// Update pod with secrets
			updatePodSpecWithSecrets(&pod.Spec, service, secretMappings)

			// Update pod with volumes
			updatePodSpecWithVolumes(&pod.Spec, service, volumeMappings, resources, project)

			resources.Pods = append(resources.Pods, pod)
			continue
		}
		// TODO InitContainer, LivenessProbe, ReadinessProbe, ImagePullSecrets, SecurityContext

		deployMode := "replicated"
		if service.Deploy != nil {
			deployMode = service.Deploy.Mode
		}
		switch deployMode {
		case "global":
			daemonSet := createDaemonSet(service)
			updatePodSpecWithSecrets(&daemonSet.Spec.Template.Spec, service, secretMappings)
			updatePodSpecWithVolumes(&daemonSet.Spec.Template.Spec, service, volumeMappings, resources, project)
			resources.DaemonSets = append(resources.DaemonSets, daemonSet)
		case "replicated":
			deployment := createDeployment(service)
			updatePodSpecWithSecrets(&deployment.Spec.Template.Spec, service, secretMappings)
			updatePodSpecWithVolumes(&deployment.Spec.Template.Spec, service, volumeMappings, resources, project)
			resources.Deployments = append(resources.Deployments, deployment)
		default:
			return nil, fmt.Errorf("unsupported deploy mode: %s", deployMode)
		}

		// Create Service if ports are defined
		if len(service.Ports) > 0 {
			resources.Services = append(resources.Services, createService(service))

			// Create Ingress if exposed
			if _, ok := service.Annotations["kubepose.service.expose"]; ok {
				resources.Ingresses = append(resources.Ingresses, createIngress(service))
			}
		}
	}

	return resources, nil
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
		Spec: createPodSpec(service),
	}
}

func createPodSpec(service types.ServiceConfig) corev1.PodSpec {
	return corev1.PodSpec{
		RestartPolicy: getRestartPolicy(service),
		Containers: []corev1.Container{
			{
				Name:       service.Name,
				Image:      service.Image,
				Command:    service.Entrypoint,
				WorkingDir: service.WorkingDir,
				Stdin:      service.StdinOpen,
				TTY:        service.Tty,
				Args:       escapeEnvs(service.Command),
				Ports:      convertPorts(service.Ports),
				Env:        convertEnvironment(service.Environment),
				Resources:  getResourceRequirements(service),
			},
		},
	}
}

func createDaemonSet(service types.ServiceConfig) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: appsv1.DaemonSetSpec{
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
				Spec: createPodSpec(service),
			},
		},
	}
}

func createDeployment(service types.ServiceConfig) *appsv1.Deployment {
	var replicas *int32
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = ptr.To(int32(*service.Deploy.Replicas))
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
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
				Spec: createPodSpec(service),
			},
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

const ServiceSelectorLabelKey = "kubepose.service"

func createService(service types.ServiceConfig) *corev1.Service {
	// TODO support LoadBalancer, NodePort, ExternalName, ClusterIP
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.Name,
			Annotations: service.Annotations,
			Labels:      service.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				ServiceSelectorLabelKey: service.Name,
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
	if class, ok := service.Annotations["kubepose.service.expose.ingress-class-name"]; ok {
		ingressClassName = &class
	}

	// Get host from labels or annotations
	host := service.Name // Default host
	if h, ok := service.Annotations["kubepose.service.expose"]; ok && h != "true" {
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
	default:
		// compose default is "no" but that is not valid in k8s deployments etc
		return corev1.RestartPolicyAlways
	}
}
