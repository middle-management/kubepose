package kubepose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	ImageRef      string
	PullPolicy    string
	IsConfigMap   bool
	IsHostPath    bool
	IsImage       bool
	IsTmpfs       bool
	TmpfsSize     *resource.Quantity
}

func (t Transformer) processVolumes(project *types.Project, resources *Resources) (map[string]VolumeMapping, error) {
	volumeMappings := make(map[string]VolumeMapping)

	for name, volume := range project.Volumes {
		if hostPath, exists := volume.Labels[VolumeHostPathLabelKey]; exists {
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

		annotations := map[string]string{}

		var storageClassName *string
		if storageClassNameLabel, ok := volume.Labels[VolumeStorageClassNameLabelKey]; ok {
			storageClassName = &storageClassNameLabel
			annotations[VolumeStorageClassNameLabelKey] = storageClassNameLabel
			delete(volume.Labels, VolumeStorageClassNameLabelKey)
		}

		requests := corev1.ResourceList{
			// default to 100Mi if not specified (same as kompose)
			corev1.ResourceStorage: resource.MustParse("100Mi"),
		}
		if size, ok := volume.Labels[VolumeSizeLabelKey]; ok {
			quantity, err := resource.ParseQuantity(size)
			if err != nil {
				return nil, fmt.Errorf("failed to parse volume size %q: %w", size, err)
			}
			requests = corev1.ResourceList{
				corev1.ResourceStorage: quantity,
			}
			annotations[VolumeSizeLabelKey] = size
			delete(volume.Labels, VolumeSizeLabelKey)
		}

		// Create PersistentVolumeClaim
		pvc := &corev1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "PersistentVolumeClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: mergeMaps(annotations, t.Annotations),
				Labels:      mergeMaps(volume.Labels, t.Labels),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: storageClassName,
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: requests,
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

func (t Transformer) updatePodSpecWithVolumes(spec *corev1.PodSpec, service types.ServiceConfig, volumeMappings map[string]VolumeMapping, resources *Resources) error {
	// Track which containers need which volumes
	containerVolumes := make(map[string][]corev1.VolumeMount)

	// Process tmpfs mounts
	for _, path := range service.Tmpfs {
		volumeName := fmt.Sprintf("tmpfs-%s", strings.NewReplacer("/", "-", ".", "-").Replace(path))

		// Add volume if it doesn't exist
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
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium: corev1.StorageMediumMemory,
					},
				},
			}

			spec.Volumes = append(spec.Volumes, volume)
		}

		// Create volume mount
		volumeMount := corev1.VolumeMount{
			Name:      volumeName,
			MountPath: path,
		}

		// Add mount to container's volume mounts
		containerVolumes[service.Name] = append(containerVolumes[service.Name], volumeMount)
	}

	// First collect volumes needed for this service
	for _, serviceVolume := range service.Volumes {
		if serviceVolume.Type == "bind" && isFilePath(serviceVolume.Source) {
			content, hash, err := readFileWithShortHash(serviceVolume.Source, volumeHmacKey)
			if err != nil {
				return fmt.Errorf("failed to read volume file %s: %w", serviceVolume.Source, err)
			}

			// Create ConfigMap if it doesn't exist
			configMapName := fmt.Sprintf("%s-%s", service.Name, hash)
			mountPath := filepath.Base(serviceVolume.Target)

			configMapExists := false
			for _, cm := range resources.ConfigMaps {
				if cm.Name == configMapName {
					configMapExists = true
					break
				}
			}

			if !configMapExists {
				configMap := &corev1.ConfigMap{
					Immutable: ptr.To(true),
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:   configMapName,
						Labels: mergeMaps(service.Labels, t.Labels),
						Annotations: mergeMaps(t.Annotations, map[string]string{
							VolumeHmacKeyAnnotationKey: volumeHmacKey,
						}),
					},
					Data: map[string]string{
						mountPath: string(content),
					},
				}
				resources.ConfigMaps = append(resources.ConfigMaps, configMap)
			}

			volumeMappings[serviceVolume.Source] = VolumeMapping{
				Name:          configMapName,
				ConfigMapName: configMapName,
				MountPath:     mountPath,
				IsConfigMap:   true,
			}
		} else if serviceVolume.Type == "image" {
			// Handle image type volumes
			// Use the source as the image reference
			volumeName := fmt.Sprintf("image-%s", strings.NewReplacer("/", "-", ":", "-", ".", "-").Replace(serviceVolume.Source))

			// Determine pull policy based on image tag (same logic as Kubernetes)
			pullPolicy := "IfNotPresent"
			if strings.HasSuffix(serviceVolume.Source, ":latest") {
				pullPolicy = "Always"
			}

			volumeMappings[serviceVolume.Source] = VolumeMapping{
				Name:       volumeName,
				ImageRef:   serviceVolume.Source,
				PullPolicy: pullPolicy,
				IsImage:    true,
			}
		}
	}

	// Process volumes for this service
	for _, serviceVolume := range service.Volumes {
		if mapping, exists := volumeMappings[serviceVolume.Source]; exists {
			volumeName := mapping.Name

			// Check if volume already exists in pod spec
			volumeExists := false
			for _, v := range spec.Volumes {
				if v.Name == volumeName {
					volumeExists = true
					break
				}
			}

			if !volumeExists {
				var volume corev1.Volume

				if mapping.IsConfigMap {
					volume = corev1.Volume{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: mapping.ConfigMapName,
								},
							},
						},
					}
				} else if mapping.IsHostPath {
					volume = corev1.Volume{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: mapping.HostPath,
							},
						},
					}
				} else if mapping.IsImage {
					// Convert pull policy string to PullPolicy type
					pullPolicy := corev1.PullPolicy(mapping.PullPolicy)
					volume = corev1.Volume{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  mapping.ImageRef,
								PullPolicy: pullPolicy,
							},
						},
					}
				} else if serviceVolume.Type == "volume" {
					volume = corev1.Volume{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ReadOnly:  serviceVolume.ReadOnly,
								ClaimName: mapping.Name,
							},
						},
					}
				} else {
					fmt.Println("unknown volume type")
					fmt.Printf("%# v\n", mapping)
					fmt.Printf("%# v\n", serviceVolume)
					continue
				}

				spec.Volumes = append(spec.Volumes, volume)
			}

			// Create volume mount for this container
			var volumeMount corev1.VolumeMount
			if mapping.IsConfigMap {
				volumeMount = corev1.VolumeMount{
					Name:      volumeName,
					MountPath: serviceVolume.Target,
					SubPath:   filepath.Base(serviceVolume.Target),
					ReadOnly:  serviceVolume.ReadOnly || mapping.IsConfigMap,
				}
			} else if mapping.IsHostPath {
				volumeMount = corev1.VolumeMount{
					Name:      volumeName,
					MountPath: serviceVolume.Target,
					ReadOnly:  true,
				}
			} else if mapping.IsImage {
				// Image volumes are always read-only
				volumeMount = corev1.VolumeMount{
					Name:        volumeName,
					MountPath:   serviceVolume.Target,
					ReadOnly:    true,
					SubPathExpr: serviceVolume.Volume.Subpath,
				}
			} else if serviceVolume.Type == "volume" {
				volumeMount = corev1.VolumeMount{
					Name:        volumeName,
					ReadOnly:    serviceVolume.ReadOnly,
					MountPath:   serviceVolume.Target,
					SubPathExpr: serviceVolume.Volume.Subpath,
				}
			}

			// Add mount to container's volume mounts
			containerVolumes[service.Name] = append(containerVolumes[service.Name], volumeMount)
		} else {
			fmt.Println("volume not found in mappings")
			fmt.Printf("%# v\n", volumeMappings)
			fmt.Printf("%# v\n", serviceVolume)
		}
	}

	// Add volume mounts only to containers that requested them
	for i := range spec.Containers {
		if mounts, exists := containerVolumes[spec.Containers[i].Name]; exists {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				mounts...,
			)
		}
	}

	// Also handle init containers
	for i := range spec.InitContainers {
		if mounts, exists := containerVolumes[spec.InitContainers[i].Name]; exists {
			spec.InitContainers[i].VolumeMounts = append(
				spec.InitContainers[i].VolumeMounts,
				mounts...,
			)
		}
	}

	return nil
}
