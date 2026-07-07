package kubepose

// Exported annotation and label keys consumed from compose service metadata.
const (
	AppSelectorLabelKey = "app.kubernetes.io/name"

	ServiceGroupAnnotationKey                  = "kubepose.service.group"
	ServiceAccountNameAnnotationKey            = "kubepose.service.serviceAccountName"
	ServiceExposeAnnotationKey                 = "kubepose.service.expose"
	ServiceExposeIngressClassNameAnnotationKey = "kubepose.service.expose.ingressClassName"
	SelectorMatchLabelsAnnotationKey           = "kubepose.selector.matchLabels"
	HealthcheckHttpGetPathAnnotationKey        = "kubepose.healthcheck.httpGet.path"
	HealthcheckHttpGetPortAnnotationKey        = "kubepose.healthcheck.httpGet.port"
	ContainerTypeAnnotationKey                 = "kubepose.container.type"

	// CronJobScheduleAnnotationKey, when set on a service, emits a CronJob
	// using the value as the cron schedule (e.g. "0 * * * *").
	CronJobScheduleAnnotationKey = "kubepose.cronjob.schedule"

	// HpaMaxReplicasAnnotationKey, when set on a service, emits an
	// autoscaling/v2 HorizontalPodAutoscaler targeting the service's
	// Deployment, scaling on average CPU utilization. The Deployment is
	// emitted without spec.replicas so re-applies don't reset the HPA's
	// chosen scale. Requires deploy.resources.reservations.cpus, since
	// utilization is a percentage of the CPU request.
	HpaMaxReplicasAnnotationKey = "kubepose.hpa.maxReplicas"
	// HpaMinReplicasAnnotationKey sets the HPA's floor. Defaults to
	// deploy.replicas, else 1.
	HpaMinReplicasAnnotationKey = "kubepose.hpa.minReplicas"
	// HpaCpuAnnotationKey sets the HPA's target average CPU utilization
	// percentage. Defaults to 80.
	HpaCpuAnnotationKey = "kubepose.hpa.cpu"

	ConfigHmacKeyAnnotationKey     = "kubepose.config.hmacKey"
	SecretHmacKeyAnnotationKey     = "kubepose.secret.hmacKey"
	VolumeHmacKeyAnnotationKey     = "kubepose.volume.hmacKey"
	VolumeHostPathLabelKey         = "kubepose.volume.hostPath"
	VolumeStorageClassNameLabelKey = "kubepose.volume.storageClassName"
	VolumeSizeLabelKey             = "kubepose.volume.size"
	SecretSubPathLabelKey          = "kubepose.secret.subPath"
)

// HMAC keys allow invalidation if the shape of an immutable
// volume/config/secret resource changes between versions.
const (
	volumeHmacKey    = "kubepose.volume.v1"
	configHmacKey    = "kubepose.config.v1"
	secretHmacKey    = "kubepose.secret.v1"
	configDefaultKey = "content"
)
