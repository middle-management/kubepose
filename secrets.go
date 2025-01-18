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

type SecretMapping struct {
	Name     string
	External bool
	SubPath  string
}

func (t Transformer) processSecrets(project *types.Project, resources *Resources) (map[string]SecretMapping, error) {
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
			hasher := hmac.New(sha256.New, []byte(secretHmacKey))
			shortHash = hex.EncodeToString(hasher.Sum(content))[0:8]
		} else if secret.File != "" {
			fileContent, fileHash, err := readFileWithShortHash(secret.File, secretHmacKey)
			if err != nil {
				return nil, fmt.Errorf("failed to read secret file %s: %w", secret.File, err)
			}
			content = fileContent
			shortHash = fileHash
		} else if secret.External {
			secretMapping[name] = SecretMapping{
				Name:     secret.Name,
				External: true,
				SubPath:  secret.Labels[SecretSubPathLabelKey],
			}
			continue
		}

		k8sSecretName := fmt.Sprintf("%s-%s", name, shortHash)
		secretMapping[name] = SecretMapping{
			Name:    k8sSecretName,
			SubPath: name,
		}

		k8sSecret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   k8sSecretName,
				Labels: secret.Labels,
				Annotations: mergeMaps(t.Annotations, map[string]string{
					SecretHmacKeyAnnotationKey: secretHmacKey,
				}),
			},
			Immutable: ptr.To(true),
			// TODO type from label kubepose.secret-type or default to Opaque
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				name: content,
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

func (t Transformer) updatePodSpecWithSecrets(spec *corev1.PodSpec, service types.ServiceConfig, secretMappings map[string]SecretMapping) {
	// Track which containers need which secrets
	containerSecrets := make(map[string][]corev1.VolumeMount)

	// First ensure all required secret volumes exist in the pod spec
	for _, serviceSecret := range service.Secrets {
		if mapping, exists := secretMappings[serviceSecret.Source]; exists {
			var optional *bool
			if mapping.External {
				optional = ptr.To(true)
			}

			// Add volume if it doesn't already exist
			volumeName := serviceSecret.Source
			volumeExists := false
			for _, v := range spec.Volumes {
				if v.Name == volumeName {
					volumeExists = true
					break
				}
			}

			if !volumeExists {
				volume := corev1.Volume{
					Name: volumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: mapping.Name,
							Optional:   optional,
						},
					},
				}
				spec.Volumes = append(spec.Volumes, volume)
			}

			// Create volume mount
			mountPath := serviceSecret.Target
			if mountPath == "" {
				mountPath = filepath.Join("/run/secrets", serviceSecret.Source)
			} else if !filepath.IsAbs(mountPath) {
				mountPath = filepath.Join("/run/secrets", mountPath)
			}

			volumeMount := corev1.VolumeMount{
				Name:      volumeName,
				ReadOnly:  true,
				MountPath: mountPath,
				SubPath:   mapping.SubPath,
			}

			// Add mount to container's secret mounts
			containerSecrets[service.Name] = append(containerSecrets[service.Name], volumeMount)
		}
	}

	// Add volume mounts only to containers that requested them
	for i := range spec.Containers {
		if mounts, exists := containerSecrets[spec.Containers[i].Name]; exists {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				mounts...,
			)
		}
	}

	// Also handle init containers
	for i := range spec.InitContainers {
		if mounts, exists := containerSecrets[spec.InitContainers[i].Name]; exists {
			spec.InitContainers[i].VolumeMounts = append(
				spec.InitContainers[i].VolumeMounts,
				mounts...,
			)
		}
	}
}
