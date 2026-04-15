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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// generateGatewayToken generates a cryptographically secure random token
// using crypto/rand. Returns a 64-character hex string (32 random bytes).
func generateGatewayToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

// applyGatewaySecret creates or updates the claw-gateway-token Secret with the gateway token
func (r *ClawResourceReconciler) applyGatewaySecret(ctx context.Context, instance *clawv1alpha1.Claw) error {
	logger := log.FromContext(ctx)

	// check if the secret already exists
	existingSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      ClawGatewaySecretName,
	}
	if err := r.Get(ctx, secretKey, existingSecret); err == nil {
		// Secret exists - check if it has the token entry
		if existingToken, exists := existingSecret.Data[GatewayTokenKeyName]; exists && len(existingToken) > 0 {
			logger.Info("Gateway secret already exists with token, skipping generation", "name", ClawGatewaySecretName)
			// no need to generate new token, just ensure owner reference is set
			return r.doCreateGatewaySecret(ctx, instance, string(existingToken))
		} else {
			// Secret exists but missing or empty token - generate new one
			logger.Info("Gateway secret exists but missing token, generating new one")
			token, err := generateGatewayToken()
			if err != nil {
				return fmt.Errorf("failed to generate gateway token: %w", err)
			}
			return r.doCreateGatewaySecret(ctx, instance, token)
		}
	} else if apierrors.IsNotFound(err) {
		// Secret doesn't exist - generate new token
		logger.Info("Gateway secret does not exist, generating new token")
		token, err := generateGatewayToken()
		if err != nil {
			return fmt.Errorf("failed to generate gateway token: %w", err)
		}
		return r.doCreateGatewaySecret(ctx, instance, token)
	} else {
		// Error fetching secret
		return fmt.Errorf("failed to check for existing gateway secret: %w", err)
	}
}

func (r *ClawResourceReconciler) doCreateGatewaySecret(ctx context.Context, instance *clawv1alpha1.Claw, token string) error {
	logger := log.FromContext(ctx)
	// Create the Secret object
	secret := &corev1.Secret{}
	secret.SetName(ClawGatewaySecretName)
	secret.SetNamespace(instance.Namespace)
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	secret.Data = map[string][]byte{
		GatewayTokenKeyName: []byte(token),
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on gateway secret: %w", err)
	}

	// Apply the Secret using server-side apply
	logger.Info("Applying gateway secret", "name", secret.Name)
	if err := r.Patch(ctx, secret, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        &[]bool{true}[0],
	}); err != nil {
		return fmt.Errorf("failed to apply gateway secret: %w", err)
	}

	logger.Info("Successfully applied gateway secret")
	return nil
}

// validateCredentials validates all credential entries: checks that referenced Secrets exist
// and that type-specific configuration is present. Returns an error describing all failures.
func (r *ClawResourceReconciler) validateCredentials(ctx context.Context, instance *clawv1alpha1.Claw) error {
	var errs []error

	for _, cred := range instance.Spec.Credentials {
		// Validate SecretRef exists for types that require it
		if cred.Type != clawv1alpha1.CredentialTypeNone {
			if cred.SecretRef == nil {
				errs = append(errs, fmt.Errorf("credential %q (type %s): secretRef is required", cred.Name, cred.Type))
				continue
			}
			secret := &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: cred.SecretRef.Name}, secret); err != nil {
				if apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("credential %q: Secret %q not found", cred.Name, cred.SecretRef.Name))
				} else {
					errs = append(errs, fmt.Errorf("credential %q: failed to get Secret %q: %w", cred.Name, cred.SecretRef.Name, err))
				}
				continue
			}
			if _, ok := secret.Data[cred.SecretRef.Key]; !ok {
				errs = append(errs, fmt.Errorf("credential %q: key %q not found in Secret %q", cred.Name, cred.SecretRef.Key, cred.SecretRef.Name))
			}
		}

		// Type-specific validation (defense-in-depth beyond CEL)
		switch cred.Type {
		case clawv1alpha1.CredentialTypeAPIKey:
			if cred.APIKey == nil {
				errs = append(errs, fmt.Errorf("credential %q: apiKey config is required for type apiKey", cred.Name))
			}
		case clawv1alpha1.CredentialTypeGCP:
			if cred.GCP == nil {
				errs = append(errs, fmt.Errorf("credential %q: gcp config is required for type gcp", cred.Name))
			}
		case clawv1alpha1.CredentialTypePathToken:
			if cred.PathToken == nil {
				errs = append(errs, fmt.Errorf("credential %q: pathToken config is required for type pathToken", cred.Name))
			}
		case clawv1alpha1.CredentialTypeOAuth2:
			if cred.OAuth2 == nil {
				errs = append(errs, fmt.Errorf("credential %q: oauth2 config is required for type oauth2", cred.Name))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("credential validation failed: %w", errors.Join(errs...))
	}
	return nil
}
