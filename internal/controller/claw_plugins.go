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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	PluginsInitContainerName = "init-plugins"
)

func pluginsEnabled(instance *clawv1alpha1.Claw) bool {
	return len(instance.Spec.Plugins) > 0
}

// pluginPackageName strips a trailing @version suffix from a plugin spec,
// returning just the package name for deduplication purposes.
func pluginPackageName(p string) string {
	// Scoped packages start with @, so find the LAST @ for the version separator.
	// "@openclaw/foo@1.2.3" → "@openclaw/foo"
	// "@openclaw/foo" → "@openclaw/foo"
	if idx := strings.LastIndex(p, "@"); idx > 0 {
		return p[:idx]
	}
	return p
}

// pluginPackageVersion returns the version part of a plugin spec, or an empty string if no version is present.
func pluginPackageVersion(p string) string {
	// Scoped packages start with @, so find the LAST @ for the version separator.
	// "@openclaw/foo@1.2.3" → "1.2.3"
	// "@openclaw/foo" → ""
	if idx := strings.LastIndex(p, "@"); idx > 0 {
		return p[idx+1:]
	}
	return ""
}

// effectivePlugins returns the complete list of plugins to install: explicit
// spec.plugins plus any implicitly required by the configured credentials
// (e.g., Vertex AI SDK providers that need an external plugin).
// Duplicates are removed by package name (spec declarations take precedence
// over implicit ones, allowing users to override the pinned version).
// Also, if the plugin is already declared in the spec with a different version,
// the version is updated to the image version.
func effectivePlugins(instance *clawv1alpha1.Claw) ([]string, error) {
	imageVersion, err := imagePluginVersion(instance.Spec.Image)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(instance.Spec.Plugins))
	merged := append([]string{}, instance.Spec.Plugins...)
	for i, p := range merged {
		pkgName := pluginPackageName(p)
		pkgVersion := pluginPackageVersion(p)
		if imageVersion != "" && pkgVersion == "" {
			merged[i] = pkgName + "@" + imageVersion
		}
		seen[pkgName] = true
	}
	implicit, err := requiredProviderPlugins(instance)
	if err != nil {
		return nil, err
	}
	for _, p := range implicit {
		if !seen[pluginPackageName(p)] {
			pkgName := pluginPackageName(p)
			if imageVersion != "" && pluginPackageVersion(p) == "" {
				p = pkgName + "@" + imageVersion
			}
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// requiredProviderPlugins inspects credentials and returns plugin package specs
// that must be installed for the configured providers to work.
// The version is derived from the image tag via imagePluginVersion; when empty
// the plugin is installed without a version pin (npm "latest").
func requiredProviderPlugins(instance *clawv1alpha1.Claw) ([]string, error) {
	version, err := imagePluginVersion(instance.Spec.Image)
	if err != nil {
		return nil, err
	}
	var plugins []string
	seen := make(map[string]bool)
	for _, cred := range instance.Spec.Credentials {
		if !usesVertexSDK(cred) {
			continue
		}
		defaults, ok := knownProviders[cred.Provider]
		if !ok || defaults.VertexPlugin == "" {
			continue
		}
		resolved := defaults.VertexPlugin
		if version != "" {
			resolved += "@" + version
		}
		if !seen[resolved] {
			plugins = append(plugins, resolved)
			seen[resolved] = true
		}
	}
	return plugins, nil
}

func generatePluginInstallScript(plugins []string) string {
	var b strings.Builder

	desiredPkgs := make([]string, 0, len(plugins))
	seenPkgs := make(map[string]bool, len(plugins))
	for _, p := range plugins {
		pkg := pluginPackageName(p)
		if !seenPkgs[pkg] {
			seenPkgs[pkg] = true
			desiredPkgs = append(desiredPkgs, pkg)
		}
	}
	// Exported (not just assigned) because the node -e snippets below read it
	// via process.env — a plain shell assignment is invisible to child processes.
	fmt.Fprintf(&b, "set -e\nexport DESIRED_PKGS=%s\n", shellQuote(strings.Join(desiredPkgs, "\n")))

	b.WriteString(`
# Some plugins (e.g. the Vertex AI SDK provider plugins) are backed by a
# scoped npm package that never materializes a directory under
# ~/.openclaw/extensions — openclaw installs their code under
# ~/.openclaw/npm/projects/<hash> instead and tracks the install record
# purely in its internal registry (persisted in ~/.openclaw's sqlite state,
# not a plain file we could diff). The $EXT-directory diff below can never
# detect these as orphaned.
#
# REGISTRY_MANIFEST is our OWN record of exactly which plugin ids the
# operator itself installed last time — never the live registry as a whole.
# This matters because the registry can also contain plugins installed
# through some other channel (e.g. a marketplace/ClawHub install done
# directly against the running instance); we must never uninstall those.
# Only ids we ourselves previously wrote to this file, and that are no
# longer desired, get uninstalled — mirroring the same safety property the
# $EXT/.operator-managed manifest below already provides for directory-based
# plugins.
REGISTRY_MANIFEST="/home/node/.openclaw/.operator-managed-plugins"
if [ -f "$REGISTRY_MANIFEST" ]; then
  node -e '
const fs = require("fs");
const desired = new Set((process.env.DESIRED_PKGS || "").split("\n").filter(Boolean));
for (const line of fs.readFileSync(process.argv[1], "utf8").split("\n")) {
  const tab = line.indexOf("\t");
  if (tab < 0) continue;
  const id = line.slice(0, tab);
  const pkg = line.slice(tab + 1);
  if (id && pkg && !desired.has(pkg)) console.log(id);
}
' "$REGISTRY_MANIFEST" | while IFS= read -r orphan_id; do
    [ -n "$orphan_id" ] && openclaw plugins uninstall "$orphan_id" --force >/dev/null 2>&1
    true
  done
fi

EXT="/home/node/.openclaw/extensions"
MANIFEST="$EXT/.operator-managed"
if [ -f "$MANIFEST" ]; then
  while IFS= read -r dir; do
    case "$dir" in
      ""|.|..|*/*|*..*) continue ;;
    esac
    target="$EXT/$dir"
    [ -e "$target" ] || continue
    rm -rf -- "$target"
  done < "$MANIFEST"
  rm -f "$MANIFEST"
else
  # No manifest from a previous successful install — clean all extension
  # dirs to avoid "plugin already exists" errors from orphaned directories
  # left by pods killed mid-install or pre-manifest operator versions.
  find "$EXT" -mindepth 1 -maxdepth 1 -type d -exec rm -rf {} + 2>/dev/null || true
fi
mkdir -p "$EXT"
# The openclaw CLI also tracks per-package install state under
# ~/.openclaw/npm/projects/<hash>, separate from $EXT above. It refuses to
# reinstall ("plugin already exists ... delete it first") if a stale project
# dir survives from a prior boot, so it's wiped unconditionally here — it's a
# pure install cache the CLI recreates from scratch on every install.
rm -rf "/home/node/.openclaw/npm/projects"
ls "$EXT" 2>/dev/null | sort > /tmp/before-plugins.txt
`)
	for _, pkg := range plugins {
		fmt.Fprintf(&b, "openclaw plugins install %s\n", shellQuote(pkg))
	}
	b.WriteString(`ls "$EXT" | sort | comm -13 /tmp/before-plugins.txt - > "$MANIFEST"

# Rebuild REGISTRY_MANIFEST from the registry, but only keep records whose
# package is one we ourselves just asked to have installed above — this is
# what guarantees the manifest can never "adopt" a plugin the operator
# didn't itself install, however it got into the registry. The file is
# written by node itself (fs.writeFileSync) rather than via shell stdout
# redirection, since the latter is not reliably flushed in all environments.
openclaw plugins registry --json 2>/dev/null | node -e '
const fs = require("fs");
const desired = new Set((process.env.DESIRED_PKGS || "").split("\n").filter(Boolean));
let data = "";
process.stdin.on("data", (c) => { data += c; });
process.stdin.on("end", () => {
  let registry;
  try { registry = JSON.parse(data); } catch { registry = {}; }
  const records = (registry.persisted && registry.persisted.installRecords) || {};
  const lines = [];
  for (const [id, rec] of Object.entries(records)) {
    const spec = rec.resolvedName || rec.spec || "";
    const idx = spec.lastIndexOf("@");
    const pkg = idx > 0 ? spec.slice(0, idx) : spec;
    if (pkg && desired.has(pkg)) lines.push(id + "\t" + pkg);
  }
  try {
    fs.writeFileSync(process.argv[1], lines.length ? lines.join("\n") + "\n" : "");
  } catch {}
});
' "$REGISTRY_MANIFEST" 2>/dev/null || true
`)
	return b.String()
}

// configurePluginsInitContainer adds an init-plugins init container to the
// gateway Deployment when plugins need to be installed. The container runs the
// openclaw CLI to install each declared plugin on the shared PVC. It goes
// through the MITM proxy (appended after wait-for-proxy).
func configurePluginsInitContainer(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	plugins []string,
) error {
	if len(plugins) == 0 {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from claw deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in claw deployment")
		}

		var gatewayImage string
		for _, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawGatewayContainerName {
				gatewayImage, _, _ = unstructured.NestedString(cm, "image")
				break
			}
		}
		if gatewayImage == "" {
			return fmt.Errorf("gateway container image not found in claw deployment")
		}

		initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")

		proxyHost := fmt.Sprintf("http://%s-proxy:8080", instance.Name)
		script := generatePluginInstallScript(plugins)

		initContainers = append(initContainers, map[string]any{
			"name":            PluginsInitContainerName,
			"image":           gatewayImage,
			"imagePullPolicy": "IfNotPresent",
			"command":         []any{"sh", "-c", script},
			"env": []any{
				map[string]any{"name": "HOME", "value": "/home/node"},
				map[string]any{"name": "NPM_CONFIG_CACHE", "value": "/home/node/.cache/npm"},
				map[string]any{"name": "HTTP_PROXY", "value": proxyHost},
				map[string]any{"name": "HTTPS_PROXY", "value": proxyHost},
				map[string]any{"name": "NO_PROXY", "value": pluginsNoProxy(instance)},
				map[string]any{"name": "NODE_EXTRA_CA_CERTS", "value": "/etc/proxy-ca/ca.crt"},
			},
			"resources": map[string]any{
				"requests": map[string]any{"memory": "128Mi", "cpu": "100m"},
				"limits":   map[string]any{"memory": "1Gi", "cpu": "500m"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"volumeMounts": []any{
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.openclaw",
					"subPath":   "home",
				},
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.local",
					"subPath":   "home/.local",
				},
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.cache",
					"subPath":   "home/.cache",
				},
				map[string]any{
					"name":      "proxy-ca",
					"mountPath": "/etc/proxy-ca",
					"readOnly":  true,
				},
				map[string]any{
					"name":      "tmp-volume",
					"mountPath": "/tmp",
				},
			},
		})

		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// pluginsNoProxy returns the NO_PROXY value for the plugins init container.
func pluginsNoProxy(instance *clawv1alpha1.Claw) string {
	base := "localhost,127.0.0.1"
	if inClusterBypassEnabled(instance) {
		return base + noProxySuffix
	}
	return base
}
