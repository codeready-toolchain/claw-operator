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
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	metricStatusReady        = "ready"
	metricStatusProvisioning = "provisioning"
	metricStatusFailed       = "failed"
)

var allStatusValues = []string{metricStatusReady, metricStatusProvisioning, metricStatusFailed}

var clawInstanceStatus = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "claw_instance_status",
		Help: "Current status of a Claw instance (1 for the active status, 0 for others)",
	},
	[]string{"status"},
)

var clawInstanceInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "claw_instance_info",
		Help: "Metadata labels for a Claw instance (always 1)",
	},
	[]string{"auth_mode", "idle"},
)

func init() {
	metrics.Registry.MustRegister(clawInstanceStatus, clawInstanceInfo)
}

func conditionReasonToStatus(reason string) string {
	switch reason {
	case clawv1alpha1.ConditionReasonReady:
		return metricStatusReady
	case clawv1alpha1.ConditionReasonValidationFailed:
		return metricStatusFailed
	default:
		return metricStatusProvisioning
	}
}

func recordClawMetrics(instance *clawv1alpha1.Claw) {
	readyCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
	var currentStatus string
	if readyCond != nil {
		currentStatus = conditionReasonToStatus(readyCond.Reason)
	} else {
		currentStatus = metricStatusProvisioning
	}

	for _, s := range allStatusValues {
		val := float64(0)
		if s == currentStatus {
			val = 1
		}
		clawInstanceStatus.WithLabelValues(s).Set(val)
	}

	clawInstanceInfo.Reset()

	authMode := string(clawv1alpha1.AuthModeToken)
	if instance.Spec.Auth != nil && instance.Spec.Auth.Mode != "" {
		authMode = string(instance.Spec.Auth.Mode)
	}

	clawInstanceInfo.WithLabelValues(
		authMode,
		strconv.FormatBool(instance.Spec.Idle),
	).Set(1)
}

func clearClawMetrics() {
	clawInstanceStatus.Reset()
	clawInstanceInfo.Reset()
}
