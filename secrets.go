package kubepose

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// processSecrets converts Compose secrets to Kubernetes secrets and returns a map of
// original secret names to their corresponding K8s secret names (with content hash)

// using a hmac key to be able to invalidate if we modify how an immutable secret is shaped
const secretsHmacKey = "kubepose.secrets.v1"
const secretsDefaultKey = "content"

type SecretMapping struct {
	Name     string
	External bool
}

func processSecrets(project *types.Project, resources *Resources) (map[string]SecretMapping, error) {
	secretMapping := make(map[string]SecretMapping)

	for name, secret := range project.Secrets {
		var content []byte
		var shortHash string
		if secret.Environment != "" {
			value, ok := os.LookupEnv(secret.Environment)
			if !ok {
				return nil, fmt.Errorf("secret %s references non-existing environment variable %s", name, secret.Environment)
			}
			content = []byte(value)
			hasher := hmac.New(sha256.New, []byte(secretsHmacKey))
			shortHash = hex.EncodeToString(hasher.Sum(content))[0:8]
		} else if secret.File != "" {
			fileContent, fileHash, err := readFileWithShortHash(secret.File, secretsHmacKey)
			if err != nil {
				return nil, fmt.Errorf("failed to read secret file %s: %w", secret.File, err)
			}
			content = fileContent
			shortHash = fileHash
		} else if secret.External {
			secretMapping[name] = SecretMapping{Name: secret.Name, External: true}
			continue
		}

		k8sSecretName := fmt.Sprintf("%s-%s", name, shortHash)
		secretMapping[name] = SecretMapping{Name: k8sSecretName}

		k8sSecret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   k8sSecretName,
				Labels: secret.Labels,
				Annotations: map[string]string{
					"generated-from":            "kubepose",
					"kubepose.original-name":    name,
					"kubepose.secrets.hmac-key": secretsHmacKey,
				},
			},
			// TODO type from label kubepose.secret-type or default to Opaque
			Immutable: ptr.To(true),
			Type:      corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				secretsDefaultKey: content,
			},
		}

		resources.Secrets = append(resources.Secrets, &k8sSecret)
	}

	return secretMapping, nil
}

// readFileWithShortHash reads the content of a secret file and its hash
func readFileWithShortHash(path string, hmacKey string) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	hasher := hmac.New(sha256.New, []byte(hmacKey))
	content, err := io.ReadAll(io.TeeReader(file, hasher))
	if err != nil {
		return nil, "", err
	}

	return content, hex.EncodeToString(hasher.Sum(nil))[0:8], nil
}

// updatePodSpecWithSecrets updates a deployment with secret volumes and mounts
func updatePodSpecWithSecrets(spec *corev1.PodSpec, service types.ServiceConfig, secretMappings map[string]SecretMapping) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Process each secret in the service
	for _, serviceSecret := range service.Secrets {
		if mapping, exists := secretMappings[serviceSecret.Source]; exists {
			var optional *bool
			if mapping.External {
				optional = ptr.To(true)
			}

			target := serviceSecret.Target
			if target == "" {
				target = filepath.Join("/run/secrets", serviceSecret.Source)
			}

			volume := corev1.Volume{
				Name: serviceSecret.Source,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: mapping.Name,
						Optional:   optional,
					},
				},
			}

			volumeMount := corev1.VolumeMount{
				Name:      volume.Name,
				MountPath: target,
				ReadOnly:  true,
			}

			// Only use SubPath for non-external secrets
			if !mapping.External {
				volumeMount.SubPath = secretsDefaultKey
			}

			volumes = append(volumes, volume)
			volumeMounts = append(volumeMounts, volumeMount)
		}
	}

	// Add volumes to pod spec if any were created
	if len(volumes) > 0 {
		spec.Volumes = append(
			spec.Volumes,
			volumes...,
		)
	}

	// Add volume mounts to container if any were created
	if len(volumeMounts) > 0 {
		for i := range spec.Containers {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				volumeMounts...,
			)
		}
	}
}
