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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// NormalizeDeployment applies the same defaults that the Kubernetes API server
// admission controller would apply. This prevents controllerutil.CreateOrUpdate
// from detecting spurious diffs between the desired spec (built by the operator)
// and the existing spec (read from the API server with defaults applied).
//
// Without this, the operator issues an Update on every reconcile even when nothing
// changed, because the API server's stored spec includes admission-defaulted fields
// that the operator's desired spec omits.
//
// Modeled on the upstream openclaw-operator's NormalizeStatefulSet.
func NormalizeDeployment(deploy *appsv1.Deployment) {
	normalizeDeploymentStrategy(deploy)

	if deploy.Spec.RevisionHistoryLimit == nil {
		deploy.Spec.RevisionHistoryLimit = ptr.To[int32](10)
	}
	if deploy.Spec.ProgressDeadlineSeconds == nil {
		deploy.Spec.ProgressDeadlineSeconds = ptr.To[int32](600)
	}

	normalizePodSpec(&deploy.Spec.Template.Spec)
}

func normalizeDeploymentStrategy(deploy *appsv1.Deployment) {
	if deploy.Spec.Strategy.Type == "" {
		deploy.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
	}
	if deploy.Spec.Strategy.Type == appsv1.RollingUpdateDeploymentStrategyType &&
		deploy.Spec.Strategy.RollingUpdate == nil {
		twentyFivePercent := intstr.FromString("25%")
		deploy.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &twentyFivePercent,
			MaxSurge:       &twentyFivePercent,
		}
	}
}

func normalizePodSpec(spec *corev1.PodSpec) {
	if spec.RestartPolicy == "" {
		spec.RestartPolicy = corev1.RestartPolicyAlways
	}
	if spec.DNSPolicy == "" {
		spec.DNSPolicy = corev1.DNSClusterFirst
	}
	if spec.SchedulerName == "" {
		spec.SchedulerName = corev1.DefaultSchedulerName
	}
	if spec.TerminationGracePeriodSeconds == nil {
		spec.TerminationGracePeriodSeconds = ptr.To[int64](30)
	}
	if spec.ServiceAccountName == "" {
		spec.ServiceAccountName = "default"
	}
	if spec.DeprecatedServiceAccount == "" {
		spec.DeprecatedServiceAccount = spec.ServiceAccountName
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	if spec.EnableServiceLinks == nil {
		spec.EnableServiceLinks = ptr.To(true)
	}

	for i := range spec.InitContainers {
		normalizeContainer(&spec.InitContainers[i])
	}
	for i := range spec.Containers {
		normalizeContainer(&spec.Containers[i])
	}

	for i := range spec.Volumes {
		normalizeVolume(&spec.Volumes[i])
	}
}

const defaultVolumeMode int32 = 0644

func normalizeVolume(v *corev1.Volume) {
	if v.ConfigMap != nil && v.ConfigMap.DefaultMode == nil {
		v.ConfigMap.DefaultMode = ptr.To(defaultVolumeMode)
	}
	if v.Secret != nil && v.Secret.DefaultMode == nil {
		v.Secret.DefaultMode = ptr.To(defaultVolumeMode)
	}
	if v.DownwardAPI != nil && v.DownwardAPI.DefaultMode == nil {
		v.DownwardAPI.DefaultMode = ptr.To(defaultVolumeMode)
	}
	if v.Projected != nil && v.Projected.DefaultMode == nil {
		v.Projected.DefaultMode = ptr.To(defaultVolumeMode)
	}
}

func normalizeContainer(c *corev1.Container) {
	for i := range c.Env {
		if c.Env[i].ValueFrom != nil && c.Env[i].ValueFrom.FieldRef != nil {
			if c.Env[i].ValueFrom.FieldRef.APIVersion == "" {
				c.Env[i].ValueFrom.FieldRef.APIVersion = "v1"
			}
		}
	}

	if c.TerminationMessagePath == "" {
		c.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if c.TerminationMessagePolicy == "" {
		c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}

	if c.ImagePullPolicy == "" {
		if strings.HasSuffix(c.Image, ":latest") || !strings.Contains(c.Image, ":") {
			c.ImagePullPolicy = corev1.PullAlways
		} else {
			c.ImagePullPolicy = corev1.PullIfNotPresent
		}
	}

	for i := range c.Ports {
		if c.Ports[i].Protocol == "" {
			c.Ports[i].Protocol = corev1.ProtocolTCP
		}
	}

	normalizeProbe(c.LivenessProbe)
	normalizeProbe(c.ReadinessProbe)
	normalizeProbe(c.StartupProbe)
}

func normalizeProbe(p *corev1.Probe) {
	if p == nil {
		return
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = 1
	}
	if p.PeriodSeconds == 0 {
		p.PeriodSeconds = 10
	}
	if p.SuccessThreshold == 0 {
		p.SuccessThreshold = 1
	}
	if p.FailureThreshold == 0 {
		p.FailureThreshold = 3
	}
	if p.HTTPGet != nil && p.HTTPGet.Scheme == "" {
		p.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
}
