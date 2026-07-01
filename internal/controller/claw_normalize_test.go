/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func makeMinimalDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "test-sa",
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "ghcr.io/test/image:v1",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
						},
					},
				},
			},
		},
	}
}

func TestNormalizeDeployment_Strategy(t *testing.T) {
	t.Run("defaults empty strategy to RollingUpdate with 25% params", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)

		assert.Equal(t, appsv1.RollingUpdateDeploymentStrategyType, deploy.Spec.Strategy.Type)
		require.NotNil(t, deploy.Spec.Strategy.RollingUpdate)
		assert.Equal(t, intstr.FromString("25%"), *deploy.Spec.Strategy.RollingUpdate.MaxUnavailable)
		assert.Equal(t, intstr.FromString("25%"), *deploy.Spec.Strategy.RollingUpdate.MaxSurge)
	})

	t.Run("does not add RollingUpdate params for Recreate strategy", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
		NormalizeDeployment(deploy)

		assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deploy.Spec.Strategy.Type)
		assert.Nil(t, deploy.Spec.Strategy.RollingUpdate)
	})

	t.Run("does not overwrite explicit RollingUpdate params", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
		custom := intstr.FromInt32(1)
		deploy.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &custom,
			MaxSurge:       &custom,
		}
		NormalizeDeployment(deploy)

		assert.Equal(t, intstr.FromInt32(1), *deploy.Spec.Strategy.RollingUpdate.MaxUnavailable)
	})
}

func TestNormalizeDeployment_DeploymentFields(t *testing.T) {
	t.Run("defaults RevisionHistoryLimit to 10", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		require.NotNil(t, deploy.Spec.RevisionHistoryLimit)
		assert.Equal(t, int32(10), *deploy.Spec.RevisionHistoryLimit)
	})

	t.Run("does not overwrite explicit RevisionHistoryLimit", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.RevisionHistoryLimit = ptr.To[int32](5)
		NormalizeDeployment(deploy)
		assert.Equal(t, int32(5), *deploy.Spec.RevisionHistoryLimit)
	})

	t.Run("defaults ProgressDeadlineSeconds to 600", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		require.NotNil(t, deploy.Spec.ProgressDeadlineSeconds)
		assert.Equal(t, int32(600), *deploy.Spec.ProgressDeadlineSeconds)
	})
}

func TestNormalizeDeployment_PodSpec(t *testing.T) {
	t.Run("defaults RestartPolicy to Always", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		assert.Equal(t, corev1.RestartPolicyAlways, deploy.Spec.Template.Spec.RestartPolicy)
	})

	t.Run("defaults DNSPolicy to ClusterFirst", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		assert.Equal(t, corev1.DNSClusterFirst, deploy.Spec.Template.Spec.DNSPolicy)
	})

	t.Run("defaults SchedulerName to default-scheduler", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		assert.Equal(t, corev1.DefaultSchedulerName, deploy.Spec.Template.Spec.SchedulerName)
	})

	t.Run("defaults TerminationGracePeriodSeconds to 30", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		require.NotNil(t, deploy.Spec.Template.Spec.TerminationGracePeriodSeconds)
		assert.Equal(t, int64(30), *deploy.Spec.Template.Spec.TerminationGracePeriodSeconds)
	})

	t.Run("copies ServiceAccountName to DeprecatedServiceAccount", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.Template.Spec.ServiceAccountName = "my-sa"
		NormalizeDeployment(deploy)
		assert.Equal(t, "my-sa", deploy.Spec.Template.Spec.DeprecatedServiceAccount)
	})

	t.Run("defaults ServiceAccountName to 'default' when empty", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.Template.Spec.ServiceAccountName = ""
		NormalizeDeployment(deploy)
		assert.Equal(t, "default", deploy.Spec.Template.Spec.ServiceAccountName)
		assert.Equal(t, "default", deploy.Spec.Template.Spec.DeprecatedServiceAccount)
	})

	t.Run("defaults EnableServiceLinks to true", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		require.NotNil(t, deploy.Spec.Template.Spec.EnableServiceLinks)
		assert.True(t, *deploy.Spec.Template.Spec.EnableServiceLinks)
	})

	t.Run("defaults SecurityContext to empty PodSecurityContext", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		NormalizeDeployment(deploy)
		assert.NotNil(t, deploy.Spec.Template.Spec.SecurityContext)
	})
}

func TestNormalizeContainer(t *testing.T) {
	t.Run("defaults TerminationMessagePath", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img:v1"}
		normalizeContainer(c)
		assert.Equal(t, corev1.TerminationMessagePathDefault, c.TerminationMessagePath)
	})

	t.Run("defaults TerminationMessagePolicy to File", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img:v1"}
		normalizeContainer(c)
		assert.Equal(t, corev1.TerminationMessageReadFile, c.TerminationMessagePolicy)
	})

	t.Run("defaults ImagePullPolicy to IfNotPresent for tagged images", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img:v1"}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
	})

	t.Run("defaults ImagePullPolicy to Always for :latest", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img:latest"}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	})

	t.Run("defaults ImagePullPolicy to Always for untagged images", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img"}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	})

	t.Run("defaults ImagePullPolicy to Always for registry-port untagged images", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "registry:5000/repo/openclaw"}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	})

	t.Run("defaults ImagePullPolicy to IfNotPresent for registry-port tagged images", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "registry:5000/repo/openclaw:v1"}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
	})

	t.Run("does not overwrite explicit ImagePullPolicy", func(t *testing.T) {
		c := &corev1.Container{Name: "test", Image: "img:v1", ImagePullPolicy: corev1.PullAlways}
		normalizeContainer(c)
		assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	})

	t.Run("defaults port Protocol to TCP", func(t *testing.T) {
		c := &corev1.Container{
			Name:  "test",
			Image: "img:v1",
			Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
		}
		normalizeContainer(c)
		assert.Equal(t, corev1.ProtocolTCP, c.Ports[0].Protocol)
	})

	t.Run("does not overwrite explicit port Protocol", func(t *testing.T) {
		c := &corev1.Container{
			Name:  "test",
			Image: "img:v1",
			Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolUDP}},
		}
		normalizeContainer(c)
		assert.Equal(t, corev1.ProtocolUDP, c.Ports[0].Protocol)
	})

	t.Run("defaults FieldRef APIVersion to v1", func(t *testing.T) {
		c := &corev1.Container{
			Name:  "test",
			Image: "img:v1",
			Env: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				},
			},
		}
		normalizeContainer(c)
		assert.Equal(t, "v1", c.Env[0].ValueFrom.FieldRef.APIVersion)
	})

	t.Run("normalizes init containers too", func(t *testing.T) {
		deploy := makeMinimalDeployment()
		deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
			{Name: "init", Image: "busybox:1.36"},
		}
		NormalizeDeployment(deploy)
		assert.Equal(t, corev1.PullIfNotPresent, deploy.Spec.Template.Spec.InitContainers[0].ImagePullPolicy)
		assert.Equal(t, corev1.TerminationMessagePathDefault, deploy.Spec.Template.Spec.InitContainers[0].TerminationMessagePath)
	})
}

func TestNormalizeProbe(t *testing.T) {
	t.Run("defaults all probe fields", func(t *testing.T) {
		p := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(8080)},
			},
		}
		normalizeProbe(p)
		assert.Equal(t, int32(1), p.TimeoutSeconds)
		assert.Equal(t, int32(10), p.PeriodSeconds)
		assert.Equal(t, int32(1), p.SuccessThreshold)
		assert.Equal(t, int32(3), p.FailureThreshold)
		assert.Equal(t, corev1.URISchemeHTTP, p.HTTPGet.Scheme)
	})

	t.Run("does not overwrite explicit probe values", func(t *testing.T) {
		p := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8080),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			TimeoutSeconds:   5,
			PeriodSeconds:    30,
			SuccessThreshold: 2,
			FailureThreshold: 10,
		}
		normalizeProbe(p)
		assert.Equal(t, int32(5), p.TimeoutSeconds)
		assert.Equal(t, int32(30), p.PeriodSeconds)
		assert.Equal(t, int32(2), p.SuccessThreshold)
		assert.Equal(t, int32(10), p.FailureThreshold)
		assert.Equal(t, corev1.URISchemeHTTPS, p.HTTPGet.Scheme)
	})

	t.Run("handles nil probe safely", func(t *testing.T) {
		normalizeProbe(nil)
	})

	t.Run("skips Scheme for non-HTTPGet probes", func(t *testing.T) {
		p := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8080)},
			},
		}
		normalizeProbe(p)
		assert.Equal(t, int32(1), p.TimeoutSeconds)
	})
}

func TestNormalizeVolume(t *testing.T) {
	t.Run("defaults ConfigMap defaultMode to 0644", func(t *testing.T) {
		v := &corev1.Volume{
			Name:         "config",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}},
		}
		normalizeVolume(v)
		require.NotNil(t, v.ConfigMap.DefaultMode)
		assert.Equal(t, int32(0644), *v.ConfigMap.DefaultMode)
	})

	t.Run("defaults Secret defaultMode to 0644", func(t *testing.T) {
		v := &corev1.Volume{
			Name:         "secret",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{}},
		}
		normalizeVolume(v)
		require.NotNil(t, v.Secret.DefaultMode)
		assert.Equal(t, int32(0644), *v.Secret.DefaultMode)
	})

	t.Run("does not overwrite explicit defaultMode", func(t *testing.T) {
		mode := int32(0600)
		v := &corev1.Volume{
			Name:         "secret",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{DefaultMode: &mode}},
		}
		normalizeVolume(v)
		assert.Equal(t, int32(0600), *v.Secret.DefaultMode)
	})

	t.Run("handles emptyDir (no defaultMode field)", func(t *testing.T) {
		v := &corev1.Volume{
			Name:         "tmp",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		}
		normalizeVolume(v)
	})
}

func TestNormalizeDeployment_Idempotent(t *testing.T) {
	deploy := makeMinimalDeployment()
	deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "init", Image: "busybox:1.36"},
	}
	deploy.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)},
		},
	}

	NormalizeDeployment(deploy)
	b1, err := json.Marshal(deploy.Spec)
	require.NoError(t, err)

	NormalizeDeployment(deploy)
	b2, err := json.Marshal(deploy.Spec)
	require.NoError(t, err)

	assert.JSONEq(t, string(b1), string(b2), "normalizing twice should produce identical output")
}

func TestNormalizeDeployment_NoSpuriousDiff(t *testing.T) {
	t.Run("minimal deployment", func(t *testing.T) {
		build := func() *appsv1.Deployment {
			deploy := makeMinimalDeployment()
			deploy.Spec.Replicas = ptr.To[int32](1)
			deploy.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
			deploy.Spec.Template.Spec.ServiceAccountName = "test-sa"
			deploy.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:  "gateway",
					Image: "ghcr.io/test/gateway:v1",
					Ports: []corev1.ContainerPort{{ContainerPort: 3000}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(3000)},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name: "POD_NAME",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
							},
						},
					},
				},
			}
			deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
				{Name: "init-config", Image: "busybox:1.36"},
			}
			return deploy
		}

		assertNoSpuriousDiff(t, build)
	})

	t.Run("realistic gateway manifest", func(t *testing.T) {
		build := func() *appsv1.Deployment {
			deploy := makeMinimalDeployment()
			deploy.Spec.Replicas = ptr.To[int32](1)
			deploy.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
			deploy.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
			deploy.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			}
			deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
				{
					Name:            "init-config",
					Image:           DefaultOpenClawImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"node", "/config/merge.js"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "claw-home", MountPath: "/home/node/.openclaw", SubPath: "home"},
						{Name: "config", MountPath: "/config"},
					},
				},
				{
					Name:            "wait-for-proxy",
					Image:           "mirror.gcr.io/library/busybox:1.37",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", "sleep 1"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
				},
			}
			deploy.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:            "gateway",
					Image:           DefaultOpenClawImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports:           []corev1.ContainerPort{{Name: "gateway", ContainerPort: 18789, Protocol: corev1.ProtocolTCP}},
					StartupProbe: &corev1.Probe{
						ProbeHandler:     corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(18789)}},
						FailureThreshold: 60,
						PeriodSeconds:    5,
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(18789)},
						},
						PeriodSeconds:    30,
						TimeoutSeconds:   15,
						FailureThreshold: 5,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(18789)},
						},
						PeriodSeconds:  10,
						TimeoutSeconds: 10,
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "claw-home", MountPath: "/home/node/.openclaw", SubPath: "home"},
						{Name: "tmp-volume", MountPath: "/tmp"},
						{Name: "proxy-ca", MountPath: "/etc/proxy-ca", ReadOnly: true},
					},
				},
			}
			deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
				{Name: "claw-home", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "test-home-pvc"},
				}},
				{Name: "config", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-config"},
					},
				}},
				{Name: "tmp-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "proxy-ca", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "test-proxy-ca"},
				}},
			}
			return deploy
		}

		assertNoSpuriousDiff(t, build)
	})

	t.Run("realistic proxy manifest", func(t *testing.T) {
		build := func() *appsv1.Deployment {
			deploy := makeMinimalDeployment()
			deploy.Spec.Replicas = ptr.To[int32](1)
			deploy.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
			deploy.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			}
			deploy.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:            "proxy",
					Image:           "claw-proxy:v1",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--config=/etc/proxy/proxy-config.json"},
					Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       15,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       10,
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsNonRoot:             ptr.To(true),
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "proxy-config", MountPath: "/etc/proxy/proxy-config.json", SubPath: "proxy-config.json", ReadOnly: true},
						{Name: "proxy-ca", MountPath: "/etc/proxy/ca", ReadOnly: true},
					},
				},
			}
			deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
				{Name: "proxy-config", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-proxy-config"},
					},
				}},
				{Name: "proxy-ca", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "test-proxy-ca"},
				}},
			}
			return deploy
		}

		assertNoSpuriousDiff(t, build)
	})
}

func assertNoSpuriousDiff(t *testing.T, build func() *appsv1.Deployment) {
	t.Helper()

	d1 := build()
	NormalizeDeployment(d1)

	data, err := json.Marshal(d1)
	require.NoError(t, err)
	existing := &appsv1.Deployment{}
	require.NoError(t, json.Unmarshal(data, existing))

	d2 := build()
	NormalizeDeployment(d2)

	mutated := existing.DeepCopy()
	mutated.Labels = d2.Labels
	mutated.Spec = d2.Spec

	assert.True(t, apiequality.Semantic.DeepEqual(existing, mutated),
		"reconcile should produce no spurious diff after normalize")
}
