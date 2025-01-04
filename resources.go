package composek8s

import (
	"fmt"
	"io"
	"sort"
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

	// Sort resources to ensure consistent output
	sort.Slice(r.ConfigMaps, func(i, j int) bool {
		return r.ConfigMaps[i].Name < r.ConfigMaps[j].Name
	})
	sort.Slice(r.Secrets, func(i, j int) bool {
		return r.Secrets[i].Name < r.Secrets[j].Name
	})
	sort.Slice(r.Deployments, func(i, j int) bool {
		return r.Deployments[i].Name < r.Deployments[j].Name
	})
	sort.Slice(r.Services, func(i, j int) bool {
		return r.Services[i].Name < r.Services[j].Name
	})

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
