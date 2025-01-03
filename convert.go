package composek8s

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func Convert(project *types.Project) (*Resources, error) {
	// Initialize K8s resources
	resources := &Resources{}

	// Process secrets first
	secretMapping, err := processSecrets(project, resources)
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
		// Create Deployment
		deployment := createDeployment(service)

		// Update deployment with secrets if any
		updateDeploymentWithSecrets(deployment, service, secretMapping)

		// Update deployment with volumes
		updateDeploymentWithVolumes(deployment, service, volumeMappings)

		// Add deployment to resources
		resources.Deployments = append(resources.Deployments, deployment)

		// Create Service if ports are defined
		if len(service.Ports) > 0 {
			k8sService := createService(service)
			resources.Services = append(resources.Services, k8sService)
		}
	}

	return resources, nil
}

func createDeployment(service types.ServiceConfig) *appsv1.Deployment {
	replicas := int32(1)
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = int32(*service.Deploy.Replicas)
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: service.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": service.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": service.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  service.Name,
							Image: service.Image,
							Ports: convertPorts(service.Ports),
							Env:   convertEnvironment(service.Environment),
						},
					},
				},
			},
		},
	}
}

func createService(service types.ServiceConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: service.Name,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": service.Name,
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
	return envVars
}
