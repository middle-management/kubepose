package kubepose

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type VolumeMapping struct {
	Name          string
	ConfigMapName string
	MountPath     string
	HostPath      string
	IsConfigMap   bool
	IsHostPath    bool
}

type VolumeExtension struct {
	HostPath string `yaml:"hostPath"`
}

func processVolumes(project *types.Project, resources *Resources) (map[string]VolumeMapping, error) {
	volumeMappings := make(map[string]VolumeMapping)

	for name, volume := range project.Volumes {
		if hostPath, exists := volume.Labels["kubepose.volume.hostPath"]; exists {
			volumeMappings[name] = VolumeMapping{
				Name:       name,
				IsHostPath: true,
				HostPath:   hostPath,
			}
			continue
		}

		// Handle regular volumes
		volumeMappings[name] = VolumeMapping{
			Name:        name,
			IsConfigMap: false,
		}

		// Create PersistentVolumeClaim
		pvc := &corev1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "PersistentVolumeClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: volume.Labels,
				Annotations: map[string]string{
					"generated-from":         "kubepose",
					"kubepose.original-name": name,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				// TODO get StorageClassName from label "kompose.volume.storage-class-name"?

				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						// TODO get from label "kompose.volume.size"?
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}
		resources.PersistentVolumeClaims = append(resources.PersistentVolumeClaims, pvc)
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

// using a hmac key to be able to invalidate if we modify how an immutable volume is shaped
const volumesHmacKey = "kubepose.volumes.v1"

func updatePodSpecWithVolumes(spec *corev1.PodSpec, service types.ServiceConfig, volumeMappings map[string]VolumeMapping, resources *Resources, project *types.Project) error {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	for _, serviceVolume := range service.Volumes {
		if serviceVolume.Type == "bind" && isFilePath(serviceVolume.Source) {
			// using a hmac key to be able to invalidate if we modify how an immutable config map is shaped
			content, hash, err := readFileWithShortHash(serviceVolume.Source, volumesHmacKey)
			if err != nil {
				return fmt.Errorf("failed to read volume file %s: %w", serviceVolume.Source, err)
			}

			projectPath, err := filepath.Rel(project.WorkingDir, serviceVolume.Source)
			if err != nil {
				return fmt.Errorf("failed to get relative path for volume file %s: %w", serviceVolume.Source, err)
			}

			// Create ConfigMap
			configMapName := fmt.Sprintf("%s-%s", service.Name, hash)
			mountPath := filepath.Base(serviceVolume.Target)
			configMap := &corev1.ConfigMap{
				Immutable: ptr.To(true),
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:   configMapName,
					Labels: service.Labels,
					Annotations: map[string]string{
						"generated-from":           "kubepose",
						"kubepose.volume.source":   projectPath,
						"kubepose.volume.hmac-key": volumesHmacKey,
					},
				},
				Data: map[string]string{
					mountPath: string(content),
				},
			}
			resources.ConfigMaps = append(resources.ConfigMaps, configMap)

			volumeMappings[serviceVolume.Source] = VolumeMapping{
				Name:          configMapName,
				ConfigMapName: configMapName,
				MountPath:     mountPath,
				IsConfigMap:   true,
			}
		}
	}

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
					SubPath:   filepath.Base(serviceVolume.Target),
					ReadOnly:  serviceVolume.ReadOnly || mapping.IsConfigMap,
				})

			} else if mapping.IsHostPath {
				// Create volume for hostPath
				volumes = append(volumes, corev1.Volume{
					Name: mapping.Name,
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: mapping.HostPath,
						},
					},
				})

				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      mapping.Name,
					MountPath: serviceVolume.Target,
					ReadOnly:  true,
				})
			} else if serviceVolume.Type == "volume" {
				volumes = append(volumes, corev1.Volume{
					Name: mapping.Name,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ReadOnly:  serviceVolume.ReadOnly,
							ClaimName: mapping.Name,
						},
					},
				})

				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:        mapping.Name,
					ReadOnly:    serviceVolume.ReadOnly,
					MountPath:   serviceVolume.Target,
					SubPathExpr: serviceVolume.Volume.Subpath,
				})
			} else {
				fmt.Println("unknown volume type")
				fmt.Printf("%# v\n", mapping)
				fmt.Printf("%# v\n", serviceVolume)
			}
		} else {
			fmt.Println("volume not found in mappings")
			fmt.Printf("%# v\n", volumeMappings)
			fmt.Printf("%# v\n", serviceVolume)
		}
	}

	// Add volumes to pod spec if any were created
	spec.Volumes = append(
		spec.Volumes,
		volumes...,
	)

	// Add volume mounts to container if any were created
	if len(volumeMounts) > 0 {
		for i := range spec.Containers {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				volumeMounts...,
			)
		}
	}

	return nil
}
