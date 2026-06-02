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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLoggingUserSecretReader(t *testing.T) {
	t.Run("nil reader returns nil", func(t *testing.T) {
		assert.Nil(t, NewLoggingUserSecretReader(nil))
	})

	t.Run("wraps non-nil reader", func(t *testing.T) {
		wrapped := NewLoggingUserSecretReader(k8sClient)
		require.NotNil(t, wrapped)
	})

	t.Run("Get delegates to underlying reader", func(t *testing.T) {
		t.Cleanup(func() {
			s := &corev1.Secret{}
			s.Name = "reader-test-secret"
			s.Namespace = namespace
			_ = k8sClient.Delete(context.Background(), s)
		})

		ctx := context.Background()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "reader-test-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		wrapped := NewLoggingUserSecretReader(k8sClient)

		result := &corev1.Secret{}
		err := wrapped.Get(ctx, client.ObjectKey{Name: "reader-test-secret", Namespace: namespace}, result)
		require.NoError(t, err)
		assert.Equal(t, "value", string(result.Data["key"]))
	})

	t.Run("Get returns error for missing secret", func(t *testing.T) {
		wrapped := NewLoggingUserSecretReader(k8sClient)

		result := &corev1.Secret{}
		err := wrapped.Get(context.Background(), client.ObjectKey{Name: "nonexistent-secret", Namespace: namespace}, result)
		require.Error(t, err)
	})

	t.Run("List delegates to underlying reader", func(t *testing.T) {
		wrapped := NewLoggingUserSecretReader(k8sClient)

		list := &corev1.SecretList{}
		err := wrapped.List(context.Background(), list, client.InNamespace(namespace))
		require.NoError(t, err)
	})
}
