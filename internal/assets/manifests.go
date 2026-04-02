package assets

import _ "embed"

//go:embed manifests/deployment.yaml
var DeploymentManifest []byte

//go:embed manifests/configmap.yaml
var ConfigMapManifest []byte
