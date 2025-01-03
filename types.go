package composek8s

import (
	"fmt"
	"io"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

type Resources struct {
	Deployments []*appsv1.Deployment
	Services    []*corev1.Service
	Secrets     []*corev1.Secret
	ConfigMaps  []*corev1.ConfigMap
	// Add other resource types as needed:
	// ConfigMaps     []corev1.ConfigMap
	// StatefulSets  []appsv1.StatefulSet
	// Ingresses     []networkingv1.Ingress
	// etc.
}

func (r *Resources) Write(writer io.Writer) error {
	var allResources []string

	// Marshal ConfigMaps
	for _, configMap := range r.ConfigMaps {
		yamlData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("error marshaling configmap: %w", err)
		}
		allResources = append(allResources, string(yamlData))
	}

	// Marshal secrets
	for _, secret := range r.Secrets {
		yamlData, err := yaml.Marshal(secret)
		if err != nil {
			return fmt.Errorf("error marshaling secret: %w", err)
		}
		allResources = append(allResources, string(yamlData))
	}

	// Marshal deployments
	for _, deployment := range r.Deployments {
		yamlData, err := yaml.Marshal(deployment)
		if err != nil {
			return fmt.Errorf("error marshaling deployment: %w", err)
		}
		allResources = append(allResources, string(yamlData))
	}

	// Marshal services
	for _, service := range r.Services {
		yamlData, err := yaml.Marshal(service)
		if err != nil {
			return fmt.Errorf("error marshaling service: %w", err)
		}
		allResources = append(allResources, string(yamlData))
	}

	// Join all resources with yaml separator
	output := strings.Join(allResources, "\n---\n")

	_, err := writer.Write([]byte(output))
	return err
}
