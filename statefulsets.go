package kubepose

import (
	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func (t Transformer) createStatefulSet(resources *Resources, service types.ServiceConfig) *appsv1.StatefulSet {
	serviceName := getServiceName(service)

	for _, s := range resources.StatefulSets {
		if s.ObjectMeta.Name == serviceName {
			return s
		}
	}

	var replicas *int32
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = ptr.To(int32(*service.Deploy.Replicas))
	}

	s := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    replicas,
			ServiceName: serviceName,
			Selector: &metav1.LabelSelector{
				MatchLabels: getMatchLabels(service),
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
	resources.StatefulSets = append(resources.StatefulSets, s)
	return s
}
