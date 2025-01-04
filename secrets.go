package composek8s

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// processSecrets converts Compose secrets to Kubernetes secrets and returns a map of
// original secret names to their corresponding K8s secret names (with content hash)

func processSecrets(project *types.Project, resources *Resources) (map[string]string, error) {
	secretMapping := make(map[string]string)

	for name, secret := range project.Secrets {
		// TODO external, env and content
		if secret.File == "" {
			secretMapping[name] = name
			continue
		}

		content, err := readSecretFile(secret.File)
		if err != nil {
			return nil, fmt.Errorf("failed to read secret file %s: %w", secret.File, err)
		}

		hash := generateHash(content)
		k8sSecretName := fmt.Sprintf("%s-%s", name, hash[:8])
		secretMapping[name] = k8sSecretName

		k8sSecret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: k8sSecretName,
				Labels: map[string]string{
					"generated-from":           "composek8s",
					"composek8s.original-name": name,
				},
			},
			// TODO type from label composek8s.secret-type or default to Opaque
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				filepath.Base(secret.File): content,
			},
		}
		for k, v := range secret.Labels {
			k8sSecret.Labels[k] = v
		}

		resources.Secrets = append(resources.Secrets, &k8sSecret)
	}

	return secretMapping, nil
}

// readSecretFile reads the content of a secret file
func readSecretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return content, nil
}

// generateHash creates a SHA256 hash of the content
func generateHash(content []byte) string {
	hasher := sha256.New()
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

// updateDeploymentWithSecrets updates a deployment with secret volumes and mounts
func updateDeploymentWithSecrets(deployment *appsv1.Deployment, service types.ServiceConfig, secretMapping map[string]string) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Process each secret in the service
	for _, serviceSecret := range service.Secrets {
		if k8sSecretName, exists := secretMapping[serviceSecret.Source]; exists {
			// Create volume for the secret
			volumeName := fmt.Sprintf("secret-%s", serviceSecret.Source)
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: k8sSecretName,
					},
				},
			})

			// Create volume mount
			target := serviceSecret.Target
			if target == "" {
				target = fmt.Sprintf("/run/secrets/%s", serviceSecret.Source)
			}

			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: target,
				ReadOnly:  true,
			})
		}
	}

	// Add volumes to pod spec if any were created
	if len(volumes) > 0 {
		deployment.Spec.Template.Spec.Volumes = append(
			deployment.Spec.Template.Spec.Volumes,
			volumes...,
		)
	}

	// Add volume mounts to container if any were created
	if len(volumeMounts) > 0 {
		for i := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(
				deployment.Spec.Template.Spec.Containers[i].VolumeMounts,
				volumeMounts...,
			)
		}
	}
}
