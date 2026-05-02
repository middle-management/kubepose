package kubepose

import (
	"github.com/compose-spec/compose-go/v2/types"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (t Transformer) createCronJob(resources *Resources, service types.ServiceConfig) *batchv1.CronJob {
	serviceName := getServiceName(service)
	schedule := service.Annotations[CronJobScheduleAnnotationKey]

	for _, c := range resources.CronJobs {
		if c.ObjectMeta.Name == serviceName {
			return c
		}
	}

	// Pods spawned by Jobs cannot use RestartPolicy=Always; coerce to OnFailure
	// when the user has not chosen Never explicitly.
	podSpec := t.createPodSpec(service)
	if podSpec.RestartPolicy == corev1.RestartPolicyAlways {
		podSpec.RestartPolicy = corev1.RestartPolicyOnFailure
	}

	c := &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Annotations: mergeMaps(service.Annotations, t.Annotations),
			Labels:      mergeMaps(service.Labels, t.Labels),
		},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: mergeMaps(service.Annotations, t.Annotations),
					Labels:      mergeMaps(service.Labels, t.Labels),
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: mergeMaps(service.Annotations, t.Annotations),
							Labels: mergeMaps(service.Labels, map[string]string{
								AppSelectorLabelKey: serviceName,
							}),
						},
						Spec: podSpec,
					},
				},
			},
		},
	}
	resources.CronJobs = append(resources.CronJobs, c)
	return c
}
