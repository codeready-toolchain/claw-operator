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

// deepMerge recursively merges override into base. Override values win on
// collision. Nested maps are merged recursively; all other types (including
// slices) are replaced. Returns a new map — neither input is mutated.
func deepMerge(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		baseVal, exists := result[k]
		if exists {
			baseMap, baseIsMap := baseVal.(map[string]any)
			overMap, overIsMap := v.(map[string]any)
			if baseIsMap && overIsMap {
				result[k] = deepMerge(baseMap, overMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

// parseUserRawConfig extracts the user's spec.config.raw as a map[string]any.
// Returns an empty map when spec.config or raw is nil/empty.
func parseUserRawConfig(instance *clawv1alpha1.Claw) (map[string]any, error) {
	if instance.Spec.Config == nil || instance.Spec.Config.Raw == nil {
		return map[string]any{}, nil
	}
	raw := instance.Spec.Config.Raw.Raw
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse spec.config.raw: %w", err)
	}
	return result, nil
}

// ensureNestedMap returns the nested map at key, creating it if absent.
func ensureNestedMap(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = map[string]any{}
		parent[key] = child
	}
	return child
}

// appendIfMissing appends val to the slice if not already present.
func appendIfMissing(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// getStringSlice extracts a []string from a nested []any.
func getStringSlice(m map[string]any, keys ...string) []string {
	var current any = m
	for _, k := range keys {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = cm[k]
	}
	arr, ok := current.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// setNestedValue sets a value at a nested key path, creating intermediate maps as needed.
func setNestedValue(m map[string]any, val any, keys ...string) {
	for i := 0; i < len(keys)-1; i++ {
		m = ensureNestedMap(m, keys[i])
	}
	m[keys[len(keys)-1]] = val
}

// stringsToAny converts []string to []any for JSON map insertion.
func stringsToAny(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// enforceInfrastructureKeys unconditionally sets always-win gateway infrastructure
// values that must match pod networking, regardless of user config.
func enforceInfrastructureKeys(config map[string]any) {
	gateway := ensureNestedMap(config, configKeyGateway)
	gateway["mode"] = "local"
	gateway["bind"] = "lan"
	gateway["port"] = float64(gatewayPort)
	ensureNestedMap(gateway, configKeyControlUI)["enabled"] = true
}

// disableUpdateCheck prevents the gateway from showing the misleading
// "update available" banner. The operator pins the container image, so
// users cannot self-update via the OpenClaw UI.
func disableUpdateCheck(config map[string]any) {
	setNestedValue(config, false, "update", "checkOnStart")
}

// enforceTrustedProxies appends the required RFC1918 ranges to any user-provided
// trustedProxies, deduplicating entries.
func enforceTrustedProxies(config map[string]any) {
	gateway := ensureNestedMap(config, configKeyGateway)
	existing := getStringSlice(config, configKeyGateway, "trustedProxies")
	existing = appendIfMissing(existing, "10.0.0.0/8")
	existing = appendIfMissing(existing, "172.16.0.0/12")
	gateway["trustedProxies"] = stringsToAny(existing)
}
