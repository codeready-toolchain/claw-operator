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
	"net/url"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// kubeconfigCluster holds parsed cluster info from a kubeconfig.
type kubeconfigCluster struct {
	Name     string
	Server   string
	Hostname string
	Port     string
	CAData   []byte
}

// kubeconfigContext holds parsed context info from a kubeconfig.
type kubeconfigContext struct {
	Name      string
	Cluster   string
	Namespace string
	Current   bool
}

// kubeconfigData holds all parsed data from a kubeconfig needed by downstream functions.
type kubeconfigData struct {
	Clusters []kubeconfigCluster
	Contexts []kubeconfigContext
	RawBytes []byte
}

// resolvedCredential wraps a CredentialSpec with parsed kubeconfig data (non-nil for kubernetes type only).
type resolvedCredential struct {
	clawv1alpha1.CredentialSpec
	KubeConfig *kubeconfigData
}

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
		Force:        ptr.To(true),
	}); err != nil {
		return fmt.Errorf("failed to apply gateway secret: %w", err)
	}

	logger.Info("Successfully applied gateway secret")
	return nil
}

// Known provider values that the controller can resolve to gateway routes.
var knownProviders = map[string]bool{
	"google":     true,
	"anthropic":  true,
	"openai":     true,
	"openrouter": true,
}

// resolveCredentials validates all credential entries and returns resolved credentials
// with parsed data (e.g., kubeconfig). Checks that referenced Secrets exist, that
// type-specific configuration is present, and that provider values are valid and unique.
func (r *ClawResourceReconciler) resolveCredentials(ctx context.Context, instance *clawv1alpha1.Claw) ([]resolvedCredential, error) {
	var errs []error
	var resolved []resolvedCredential
	seenProviders := map[string]string{} // provider -> credential name

	for _, cred := range instance.Spec.Credentials {
		rc := resolvedCredential{CredentialSpec: cred}

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
			data, ok := secret.Data[cred.SecretRef.Key]
			if !ok {
				errs = append(errs, fmt.Errorf("credential %q: key %q not found in Secret %q", cred.Name, cred.SecretRef.Key, cred.SecretRef.Name))
				continue
			}

			if cred.Type == clawv1alpha1.CredentialTypeKubernetes {
				kd, err := parseAndValidateKubeconfig(data)
				if err != nil {
					errs = append(errs, fmt.Errorf("credential %q: %w", cred.Name, err))
					continue
				}
				rc.KubeConfig = kd
			}
		}

		// Validate provider field
		if cred.Provider != "" {
			if !knownProviders[cred.Provider] {
				errs = append(errs, fmt.Errorf("credential %q: unknown provider %q (known: google, anthropic, openai, openrouter)", cred.Name, cred.Provider))
			}
			if existing, seen := seenProviders[cred.Provider]; seen {
				errs = append(errs, fmt.Errorf("credential %q: duplicate provider %q (already used by credential %q)", cred.Name, cred.Provider, existing))
			} else {
				seenProviders[cred.Provider] = cred.Name
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

		resolved = append(resolved, rc)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("credential validation failed: %w", errors.Join(errs...))
	}
	return resolved, nil
}

// parseAndValidateKubeconfig parses kubeconfig bytes and validates that all users
// use token-based auth. Returns extracted cluster and context metadata.
func parseAndValidateKubeconfig(data []byte) (*kubeconfigData, error) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Validate all users use inline token auth only — reject file references and
	// other auth mechanisms that won't work inside the proxy container.
	for userName, authInfo := range cfg.AuthInfos {
		if len(authInfo.ClientCertificateData) > 0 || authInfo.ClientCertificate != "" {
			return nil, fmt.Errorf("user %q uses client certificate auth (not supported, use token auth)", userName)
		}
		if authInfo.Exec != nil {
			return nil, fmt.Errorf("user %q uses exec-based auth (not supported, use token auth)", userName)
		}
		if authInfo.AuthProvider != nil {
			return nil, fmt.Errorf("user %q uses auth provider (not supported, use token auth)", userName)
		}
		if authInfo.TokenFile != "" {
			return nil, fmt.Errorf("user %q uses token-file auth (not supported, inline the token directly)", userName)
		}
		if authInfo.Username != "" || authInfo.Password != "" {
			return nil, fmt.Errorf("user %q uses basic auth (not supported, use token auth)", userName)
		}
		if authInfo.Token == "" {
			return nil, fmt.Errorf("user %q has no token configured", userName)
		}
	}

	// Parse clusters and validate server URLs
	serverTokens := map[string]string{} // "hostname:port" -> token (for uniqueness check)
	var clusters []kubeconfigCluster
	for clusterName, cluster := range cfg.Clusters {
		if cluster.Server == "" {
			return nil, fmt.Errorf("cluster %q has no server URL", clusterName)
		}
		if cluster.CertificateAuthority != "" {
			return nil, fmt.Errorf("cluster %q uses certificate-authority file path (not supported, "+
				"inline the CA with certificate-authority-data instead)", clusterName)
		}
		hostname, port, err := parseServerURL(cluster.Server)
		if err != nil {
			return nil, fmt.Errorf("cluster %q: %w", clusterName, err)
		}
		clusters = append(clusters, kubeconfigCluster{
			Name:     clusterName,
			Server:   cluster.Server,
			Hostname: hostname,
			Port:     port,
			CAData:   cluster.CertificateAuthorityData,
		})
	}

	// Validate one-token-per-server: build server -> token mapping via contexts
	for ctxName, ctxInfo := range cfg.Contexts {
		cluster, ok := cfg.Clusters[ctxInfo.Cluster]
		if !ok {
			continue
		}
		authInfo, ok := cfg.AuthInfos[ctxInfo.AuthInfo]
		if !ok {
			continue
		}
		hostname, port, err := parseServerURL(cluster.Server)
		if err != nil {
			continue
		}
		key := hostname + ":" + port
		token := authInfo.Token
		if existing, seen := serverTokens[key]; seen && existing != token {
			return nil, fmt.Errorf("context %q: server %s has conflicting tokens from different users "+
				"(split into separate kubeconfigs or use the same user)", ctxName, key)
		}
		serverTokens[key] = token
	}

	// Parse contexts
	var contexts []kubeconfigContext
	for ctxName, ctxInfo := range cfg.Contexts {
		contexts = append(contexts, kubeconfigContext{
			Name:      ctxName,
			Cluster:   ctxInfo.Cluster,
			Namespace: ctxInfo.Namespace,
			Current:   ctxName == cfg.CurrentContext,
		})
	}

	return &kubeconfigData{
		Clusters: clusters,
		Contexts: contexts,
		RawBytes: data,
	}, nil
}

// parseServerURL extracts hostname and port from a Kubernetes API server URL.
// Defaults port to "443" when not specified.
func parseServerURL(server string) (string, string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", "", fmt.Errorf("invalid server URL %q: %w", server, err)
	}
	hostname := u.Hostname()
	if hostname == "" {
		return "", "", fmt.Errorf("server URL %q has no hostname", server)
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return hostname, port, nil
}

// sanitizeKubeconfig replaces all user tokens with a placeholder and returns
// the sanitized kubeconfig YAML bytes.
func sanitizeKubeconfig(rawBytes []byte) ([]byte, error) {
	cfg, err := clientcmd.Load(rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig for sanitization: %w", err)
	}

	for _, authInfo := range cfg.AuthInfos {
		if authInfo.Token != "" {
			authInfo.Token = "proxy-managed-token"
		}
		if authInfo.TokenFile != "" {
			authInfo.TokenFile = ""
			authInfo.Token = "proxy-managed-token"
		}
	}

	return clientcmd.Write(*cfg)
}

// applySanitizedKubeconfig creates or updates the sanitized kubeconfig ConfigMap
// for the gateway pod. Only created when a kubernetes credential is present.
func (r *ClawResourceReconciler) applySanitizedKubeconfig(ctx context.Context, instance *clawv1alpha1.Claw, resolvedCreds []resolvedCredential) error {
	var kd *kubeconfigData
	for i := range resolvedCreds {
		if resolvedCreds[i].KubeConfig != nil {
			kd = resolvedCreds[i].KubeConfig
			break
		}
	}
	if kd == nil {
		existing := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: ClawKubeConfigMapName, Namespace: instance.Namespace}, existing); err == nil {
			log.FromContext(ctx).Info("Cleaning up orphaned kubeconfig ConfigMap")
			return r.Delete(ctx, existing)
		}
		return nil
	}

	logger := log.FromContext(ctx)

	sanitized, err := sanitizeKubeconfig(kd.RawBytes)
	if err != nil {
		return fmt.Errorf("failed to sanitize kubeconfig: %w", err)
	}

	cm := &corev1.ConfigMap{}
	cm.SetName(ClawKubeConfigMapName)
	cm.SetNamespace(instance.Namespace)
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	cm.Data = map[string]string{
		"config": string(sanitized),
	}

	if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on kubeconfig ConfigMap: %w", err)
	}

	if err := r.Patch(ctx, cm, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		return fmt.Errorf("failed to apply kubeconfig ConfigMap: %w", err)
	}

	logger.Info("Applied sanitized kubeconfig ConfigMap")
	return nil
}

// hasKubernetesCredentials returns true if any resolved credential is type kubernetes.
func hasKubernetesCredentials(creds []resolvedCredential) bool {
	for i := range creds {
		if creds[i].Type == clawv1alpha1.CredentialTypeKubernetes {
			return true
		}
	}
	return false
}
