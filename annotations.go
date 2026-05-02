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
	ConfigHmacKeyAnnotationKey                 = "kubepose.config.hmacKey"
	SecretHmacKeyAnnotationKey                 = "kubepose.secret.hmacKey"
	VolumeHmacKeyAnnotationKey                 = "kubepose.volume.hmacKey"
	VolumeHostPathLabelKey                     = "kubepose.volume.hostPath"
	VolumeStorageClassNameLabelKey             = "kubepose.volume.storageClassName"
	VolumeSizeLabelKey                         = "kubepose.volume.size"
	SecretSubPathLabelKey                      = "kubepose.secret.subPath"
)

// HMAC keys allow invalidation if the shape of an immutable
// volume/config/secret resource changes between versions.
const (
	volumeHmacKey    = "kubepose.volume.v1"
	configHmacKey    = "kubepose.config.v1"
	secretHmacKey    = "kubepose.secret.v1"
	configDefaultKey = "content"
)
