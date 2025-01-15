package kubepose

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// using a hmac key to be able to invalidate if we modify how an immutable config is shaped
const configsHmacKey = "kubepose.configs.v1"
const configsDefaultKey = "content"

type ConfigMapping struct {
	Name     string
	External bool
}

func processConfigs(project *types.Project, resources *Resources) (map[string]ConfigMapping, error) {
	configMapping := make(map[string]ConfigMapping)

	for name, config := range project.Configs {
		if config.External {
			configMapping[name] = ConfigMapping{Name: config.Name, External: true}
			continue
		}

		var content []byte
		var shortHash string
		var filename string

		switch {
		case config.Content != "":
			content = []byte(config.Content)
			_, shortHash = getContentHash(content, configsHmacKey)
			filename = configsDefaultKey

		case config.Environment != "":
			value, ok := os.LookupEnv(config.Environment)
			if !ok {
				return nil, fmt.Errorf("config %s references non-existing environment variable %s", name, config.Environment)
			}
			content = []byte(value)
			_, shortHash = getContentHash(content, configsHmacKey)
			filename = configsDefaultKey

		case config.File != "":
			fileContent, fileHash, err := readFileWithShortHash(config.File, configsHmacKey)
			if err != nil {
				return nil, fmt.Errorf("failed to read config file %s: %w", config.File, err)
			}
			content = fileContent
			shortHash = fileHash
			filename = filepath.Base(config.File)

		default:
			return nil, fmt.Errorf("config %s must specify either content, file or environment", name)
		}

		k8sConfigName := fmt.Sprintf("%s-%s", name, shortHash)
		configMapping[name] = ConfigMapping{Name: k8sConfigName}

		k8sConfigMap := corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   k8sConfigName,
				Labels: config.Labels,
				Annotations: map[string]string{
					"generated-from":            "kubepose",
					"kubepose.original-name":    name,
					"kubepose.configs.hmac-key": configsHmacKey,
				},
			},
			Immutable: ptr.To(true),
			Data: map[string]string{
				filename: string(content),
			},
		}

		resources.ConfigMaps = append(resources.ConfigMaps, &k8sConfigMap)
	}

	return configMapping, nil
}

func updatePodSpecWithConfigs(spec *corev1.PodSpec, service types.ServiceConfig, configMappings map[string]ConfigMapping) {
	// Track which containers need which configs
	containerConfigs := make(map[string][]corev1.VolumeMount)

	// Process each config in the service
	for _, serviceConfig := range service.Configs {
		if mapping, exists := configMappings[serviceConfig.Source]; exists {
			var optional *bool
			if mapping.External {
				optional = ptr.To(true)
			}

			// Set target according to Docker Compose defaults
			target := serviceConfig.Target
			if target == "" {
				// TODO: Add Windows support by checking container OS
				target = "/" + serviceConfig.Source
			}

			// Create volume if it doesn't already exist
			volumeName := serviceConfig.Source
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
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: mapping.Name,
							},
							Optional: optional,
						},
					},
				}

				if !mapping.External {
					// For non-external configs, mount only the specific key
					volume.VolumeSource.ConfigMap.Items = []corev1.KeyToPath{
						{
							Key:  configsDefaultKey,
							Path: filepath.Base(target),
						},
					}
				}

				spec.Volumes = append(spec.Volumes, volume)
			}

			volumeMount := corev1.VolumeMount{
				Name:      volumeName,
				MountPath: target,
				ReadOnly:  true,
			}

			// Add mount to container's config mounts
			containerConfigs[service.Name] = append(containerConfigs[service.Name], volumeMount)
		}
	}

	// Add volume mounts only to containers that requested them
	for i := range spec.Containers {
		if mounts, exists := containerConfigs[spec.Containers[i].Name]; exists {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				mounts...,
			)
		}
	}

	// Also handle init containers
	for i := range spec.InitContainers {
		if mounts, exists := containerConfigs[spec.InitContainers[i].Name]; exists {
			spec.InitContainers[i].VolumeMounts = append(
				spec.InitContainers[i].VolumeMounts,
				mounts...,
			)
		}
	}
}

// Helper function to get content hash
func getContentHash(content []byte, hmacKey string) ([]byte, string) {
	hasher := hmac.New(sha256.New, []byte(hmacKey))
	hasher.Write(content)
	hash := hasher.Sum(nil)
	return hash, hex.EncodeToString(hash)[0:8]
}
