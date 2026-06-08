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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func resetMetrics() {
	clawInstanceStatus.Reset()
	clawInstanceInfo.Reset()
}

func TestRecordClawMetrics(t *testing.T) {
	t.Run("should set ready=1 when condition reason is Ready", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusReady,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusProvisioning,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusFailed,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusIdled,
		})))
	})

	t.Run("should set provisioning=1 when condition reason is Provisioning", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: clawv1alpha1.ConditionReasonProvisioning},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusReady,
		})))
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusProvisioning,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusFailed,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusIdled,
		})))
	})

	t.Run("should set failed=1 when condition reason is ValidationFailed", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: clawv1alpha1.ConditionReasonValidationFailed},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusReady,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusProvisioning,
		})))
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusFailed,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusIdled,
		})))
	})

	t.Run("should default to provisioning when no Ready condition exists", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusReady,
		})))
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusProvisioning,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusFailed,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusIdled,
		})))
	})

	t.Run("should set idled=1 when condition reason is Idle", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: clawv1alpha1.ConditionReasonIdle},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusReady,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusProvisioning,
		})))
		assert.Equal(t, float64(0), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusFailed,
		})))
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "status": metricStatusIdled,
		})))
	})
}

func TestClearClawMetrics(t *testing.T) {
	t.Run("should remove all series for the given instance", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}
		recordClawMetrics(instance)

		assert.Equal(t, 4, testutil.CollectAndCount(clawInstanceStatus))
		assert.Equal(t, 1, testutil.CollectAndCount(clawInstanceInfo))

		clearClawMetrics("my-claw", "test-ns")

		assert.Equal(t, 0, testutil.CollectAndCount(clawInstanceStatus))
		assert.Equal(t, 0, testutil.CollectAndCount(clawInstanceInfo))
	})

	t.Run("should only remove series for the specified instance", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance1 := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "claw-1", Namespace: "ns-1"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}
		instance2 := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "claw-2", Namespace: "ns-2"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: clawv1alpha1.ConditionReasonProvisioning},
				},
			},
		}
		recordClawMetrics(instance1)
		recordClawMetrics(instance2)

		assert.Equal(t, 8, testutil.CollectAndCount(clawInstanceStatus))
		assert.Equal(t, 2, testutil.CollectAndCount(clawInstanceInfo))

		clearClawMetrics("claw-1", "ns-1")

		assert.Equal(t, 4, testutil.CollectAndCount(clawInstanceStatus))
		assert.Equal(t, 1, testutil.CollectAndCount(clawInstanceInfo))
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceStatus.With(prometheus.Labels{
			"name": "claw-2", "namespace": "ns-2", "status": metricStatusProvisioning,
		})))
	})
}

func TestClawInstanceInfoMetric(t *testing.T) {
	t.Run("should set info metric with default auth mode", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceInfo.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "auth_mode": "token", "idle": "false",
		})))
	})

	t.Run("should set info metric with password auth mode", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"}},
			},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceInfo.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "auth_mode": "password", "idle": "false",
		})))
	})

	t.Run("should reflect metrics enabled and idle", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Spec: clawv1alpha1.ClawSpec{
				Metrics: &clawv1alpha1.MetricsSpec{Enabled: true},
				Idle:    true,
			},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: clawv1alpha1.ConditionReasonIdle},
				},
			},
		}

		recordClawMetrics(instance)

		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceInfo.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "auth_mode": "token", "idle": "true",
		})))
	})

	t.Run("should clean stale info labels on spec change", func(t *testing.T) {
		t.Cleanup(resetMetrics)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "my-claw", Namespace: "test-ns"},
			Status: clawv1alpha1.ClawStatus{
				Conditions: []metav1.Condition{
					{Type: clawv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: clawv1alpha1.ConditionReasonReady},
				},
			},
		}
		recordClawMetrics(instance)

		assert.Equal(t, 1, testutil.CollectAndCount(clawInstanceInfo))

		instance.Spec.Idle = true
		recordClawMetrics(instance)

		assert.Equal(t, 1, testutil.CollectAndCount(clawInstanceInfo),
			"old series should be removed, only one info series should exist")
		assert.Equal(t, float64(1), testutil.ToFloat64(clawInstanceInfo.With(prometheus.Labels{
			"name": "my-claw", "namespace": "test-ns", "auth_mode": "token", "idle": "true",
		})))
	})
}

func TestConditionReasonToStatus(t *testing.T) {
	tests := []struct {
		reason   string
		expected string
	}{
		{clawv1alpha1.ConditionReasonReady, metricStatusReady},
		{clawv1alpha1.ConditionReasonProvisioning, metricStatusProvisioning},
		{clawv1alpha1.ConditionReasonValidationFailed, metricStatusFailed},
		{clawv1alpha1.ConditionReasonIdle, metricStatusIdled},
		{"UnknownReason", metricStatusProvisioning},
		{"", metricStatusProvisioning},
	}

	for _, tt := range tests {
		t.Run("reason="+tt.reason, func(t *testing.T) {
			assert.Equal(t, tt.expected, conditionReasonToStatus(tt.reason))
		})
	}
}
