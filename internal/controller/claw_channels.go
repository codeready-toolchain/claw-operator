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
	"fmt"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// channelSecretRole defines a secret role with its placeholder token for proxy injection.
type channelSecretRole struct {
	Role         string
	Placeholder  string
	EnvVarSuffix string
	AllowedPaths []string
}

// channelDefault holds the inferred proxy and config defaults for a known messaging channel.
type channelDefault struct {
	Type        clawv1alpha1.CredentialType
	Domain      string
	APIKey      *clawv1alpha1.APIKeyConfig
	PathToken   *clawv1alpha1.PathTokenConfig
	Companions  []string // additional domains to allowlist (type: none)
	SecretRoles []channelSecretRole

	// ConfigBase is the base channel config block injected into operator.json.
	// Keys like "enabled" and token placeholders are added by buildChannelConfig.
	ConfigBase map[string]any
}

// protectedChannelKeys are operator-managed keys that must not appear in user channelConfig.
var protectedChannelKeys = map[string]bool{
	"enabled":  true,
	"botToken": true,
	"token":    true,
	"appToken": true,
}

// knownChannels maps channel names to their inferred defaults.
var knownChannels = map[string]channelDefault{
	"telegram": {
		Type:      clawv1alpha1.CredentialTypePathToken,
		Domain:    "api.telegram.org",
		PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"},
		SecretRoles: []channelSecretRole{
			{Placeholder: "placeholder"},
		},
		ConfigBase: map[string]any{
			"botToken":  "placeholder",
			"dmPolicy":  "open",
			"allowFrom": []any{"*"},
		},
	},
	"discord": {
		Type:   clawv1alpha1.CredentialTypeAPIKey,
		Domain: "discord.com",
		APIKey: &clawv1alpha1.APIKeyConfig{Header: "Authorization", ValuePrefix: "Bot "},
		Companions: []string{
			"gateway.discord.gg",
			"cdn.discordapp.com",
		},
		SecretRoles: []channelSecretRole{
			{Placeholder: "placeholder"},
		},
		ConfigBase: map[string]any{
			"botToken": "placeholder",
		},
	},
	"slack": {
		Type:   clawv1alpha1.CredentialTypeBearer,
		Domain: "slack.com",
		Companions: []string{
			".slack.com",
		},
		SecretRoles: []channelSecretRole{
			{Role: "appToken", Placeholder: "xapp-placeholder", EnvVarSuffix: "APP", AllowedPaths: []string{"/api/apps.connections.open"}},
			{Role: "botToken", Placeholder: "xoxb-placeholder", EnvVarSuffix: "BOT"},
		},
		ConfigBase: map[string]any{
			"botToken": "xoxb-placeholder",
			"appToken": "xapp-placeholder",
		},
	},
	"whatsapp": {
		Type: clawv1alpha1.CredentialTypeNone,
		Companions: []string{
			".whatsapp.com",
			".whatsapp.net",
			".facebook.com",
			".facebook.net",
			".fbcdn.net",
		},
		ConfigBase: map[string]any{},
	},
}

// resolveChannelDefaults fills in missing Type, Domain, and type-specific config for
// known channels. Explicit user values are preserved (escape hatch).
func resolveChannelDefaults(cred *clawv1alpha1.CredentialSpec) error {
	defaults, ok := knownChannels[cred.Channel]
	if !ok {
		return fmt.Errorf("credential %q: unknown channel %q (known: telegram, discord, slack, whatsapp)",
			cred.Name, cred.Channel)
	}

	if cred.Type == "" {
		cred.Type = defaults.Type
	}
	if cred.Domain == "" {
		cred.Domain = defaults.Domain
	}
	if cred.PathToken == nil && defaults.PathToken != nil {
		cred.PathToken = defaults.PathToken
	}
	if cred.APIKey == nil && defaults.APIKey != nil {
		cred.APIKey = defaults.APIKey
	}

	return nil
}

// generateCompanionRoutes returns proxy routes (type=none) for companion domains
// that a channel needs beyond its primary domain.
func generateCompanionRoutes(cred clawv1alpha1.CredentialSpec) []proxyRoute {
	if cred.Channel == "" {
		return nil
	}
	defaults, ok := knownChannels[cred.Channel]
	if !ok || len(defaults.Companions) == 0 {
		return nil
	}

	routes := make([]proxyRoute, 0, len(defaults.Companions))
	for _, domain := range defaults.Companions {
		routes = append(routes, proxyRoute{
			Domain:   domain,
			Injector: "none",
		})
	}
	return routes
}

// validateChannelConfig checks that user-provided channelConfig does not contain
// protected keys managed by the operator.
func validateChannelConfig(credName string, raw []byte) error {
	var userConfig map[string]any
	if err := json.Unmarshal(raw, &userConfig); err != nil {
		return fmt.Errorf("credential %q: channelConfig is not valid JSON: %w", credName, err)
	}
	for key := range userConfig {
		if protectedChannelKeys[key] {
			return fmt.Errorf("credential %q: channelConfig key %q is operator-managed and cannot be overridden",
				credName, key)
		}
	}
	return nil
}

// buildChannelConfig constructs the complete channel config block for operator.json.
// It starts with {"enabled": true}, adds the channel's base config (placeholders etc.),
// then deep-merges the user's channelConfig on top.
func buildChannelConfig(cred clawv1alpha1.CredentialSpec) (map[string]any, error) {
	defaults, ok := knownChannels[cred.Channel]
	if !ok {
		return nil, fmt.Errorf("credential %q: unknown channel %q", cred.Name, cred.Channel)
	}

	config := map[string]any{"enabled": true}
	for k, v := range defaults.ConfigBase {
		config[k] = v
	}

	if cred.ChannelConfig != nil && len(cred.ChannelConfig.Raw) > 0 {
		if err := validateChannelConfig(cred.Name, cred.ChannelConfig.Raw); err != nil {
			return nil, err
		}

		var userConfig map[string]any
		if err := json.Unmarshal(cred.ChannelConfig.Raw, &userConfig); err != nil {
			return nil, fmt.Errorf("credential %q: failed to parse channelConfig: %w", cred.Name, err)
		}
		config = deepMergeMap(config, userConfig)
	}

	return config, nil
}

// deepMergeMap recursively merges src into dst. Objects are deep-merged,
// arrays are replaced wholesale, scalars are overwritten by src.
func deepMergeMap(dst, src map[string]any) map[string]any {
	result := make(map[string]any, len(dst))
	for k, v := range dst {
		result[k] = v
	}
	for k, v := range src {
		if srcMap, srcIsMap := v.(map[string]any); srcIsMap {
			if dstMap, dstIsMap := result[k].(map[string]any); dstIsMap {
				result[k] = deepMergeMap(dstMap, srcMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

// injectChannels injects channel configuration and plugin entries for all
// credentials that have the channel field set. Always-win for declared
// channels; user-managed plugin entries for non-declared keys are preserved.
func injectChannels(config map[string]any, instance *clawv1alpha1.Claw) error {
	channels := map[string]any{}
	plugins := map[string]any{}

	for _, cred := range instance.Spec.Credentials {
		if cred.Channel == "" {
			continue
		}

		channelConfig, err := buildChannelConfig(cred)
		if err != nil {
			return err
		}
		channels[cred.Channel] = channelConfig
		plugins[cred.Channel] = map[string]any{"enabled": true}
	}

	if len(channels) == 0 {
		return nil
	}

	config["channels"] = channels

	existingEntries := ensureNestedMap(ensureNestedMap(config, "plugins"), "entries")
	for k, v := range plugins {
		existingEntries[k] = v
	}
	return nil
}
