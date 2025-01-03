package composek8s

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VolumeMapping struct {
	Name          string
	ConfigMapName string
	MountPath     string
	IsConfigMap   bool
}

func processVolumes(project *types.Project, resources *Resources) (map[string]VolumeMapping, error) {
	volumeMappings := make(map[string]VolumeMapping)

	for name, volume := range project.Volumes {
		// If the volume has a driver or external flag, skip it as it might be a named volume
		if volume.Driver != "" || volume.External {
			continue
		}

		// Check if the volume name is actually a file path
		if isFilePath(volume.Name) {
			configMapName := fmt.Sprintf("%s-config", name)
			content, err := readFile(volume.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to read volume file %s: %w", volume.Name, err)
			}

			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: configMapName,
					Labels: map[string]string{
						"generated-from": "compose",
					},
				},
				Data: map[string]string{
					filepath.Base(volume.Name): string(content),
				},
			}

			resources.ConfigMaps = append(resources.ConfigMaps, configMap)

			volumeMappings[name] = VolumeMapping{
				Name:          name,
				ConfigMapName: configMapName,
				MountPath:     volume.Name, // Use the original path as mount path
				IsConfigMap:   true,
			}
		} else {
			// Handle regular volumes
			volumeMappings[name] = VolumeMapping{
				Name:        name,
				IsConfigMap: false,
			}
		}
	}

	return volumeMappings, nil
}

func isFilePath(path string) bool {
	// Check if the path exists and is a file
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

func updateDeploymentWithVolumes(deployment *appsv1.Deployment, service types.ServiceConfig, volumeMappings map[string]VolumeMapping) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	for _, serviceVolume := range service.Volumes {
		if mapping, exists := volumeMappings[serviceVolume.Source]; exists {
			if mapping.IsConfigMap {
				// Create volume for ConfigMap
				volumes = append(volumes, corev1.Volume{
					Name: mapping.Name,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: mapping.ConfigMapName,
							},
						},
					},
				})

				// Create volume mount
				mountPath := serviceVolume.Target
				if mountPath == "" {
					mountPath = mapping.MountPath
				}

				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      mapping.Name,
					MountPath: mountPath,
					ReadOnly:  serviceVolume.ReadOnly,
				})
			} else {
				// Handle other volume types if needed
				// This could include persistent volumes, empty dir, etc.
			}
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
