package composek8s

import (
	"fmt"
	"io"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

type Resources struct {
	Deployments            []*appsv1.Deployment
	Services               []*corev1.Service
	Secrets                []*corev1.Secret
	ConfigMaps             []*corev1.ConfigMap
	PersistentVolumes      []*corev1.PersistentVolume
	PersistentVolumeClaims []*corev1.PersistentVolumeClaim
}

type k8sObject interface {
	runtime.Object
	metav1.Object
}

func toObjects[T k8sObject](items []T) []k8sObject {
	result := make([]k8sObject, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

func (r *Resources) Write(writer io.Writer) error {
	var items []k8sObject
	items = append(items, toObjects(r.ConfigMaps)...)
	items = append(items, toObjects(r.Secrets)...)
	items = append(items, toObjects(r.Deployments)...)
	items = append(items, toObjects(r.Services)...)
	items = append(items, toObjects(r.PersistentVolumes)...)
	items = append(items, toObjects(r.PersistentVolumeClaims)...)

	sort.Slice(items, func(i, j int) bool {
		ki := items[i].GetObjectKind().GroupVersionKind().Kind
		kj := items[j].GetObjectKind().GroupVersionKind().Kind
		if ki != kj {
			return ki < kj
		}
		ni := items[i].GetName()
		nj := items[j].GetName()
		return ni < nj
	})

	var allResources []string
	for _, item := range items {
		yamlData, err := yaml.Marshal(item)
		if err != nil {
			return fmt.Errorf("error marshaling item: %w", err)
		}
		allResources = append(allResources, string(yamlData))
	}

	// Join all resources with yaml separator
	output := strings.Join(allResources, "\n---\n")

	_, err := writer.Write([]byte(output))
	return err
}
