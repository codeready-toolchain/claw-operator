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

package proxy

import (
	"fmt"
	"os"
	"strings"
)

type credentialSource interface {
	Value() (string, error)
}

type envCredentialSource struct {
	name string
}

type fileCredentialSource struct {
	path string
}

func newCredentialSource(route *Route, injectorName string) (credentialSource, error) {
	hasEnvVar := route.EnvVar != ""
	hasFile := route.CredentialFile != ""
	switch {
	case hasEnvVar && hasFile:
		return nil, fmt.Errorf("%s injector requires exactly one of envVar or credentialFile", injectorName)
	case hasEnvVar:
		return envCredentialSource{name: route.EnvVar}, nil
	case hasFile:
		return fileCredentialSource{path: route.CredentialFile}, nil
	default:
		return nil, fmt.Errorf("%s injector requires envVar or credentialFile", injectorName)
	}
}

func (e envCredentialSource) Value() (string, error) {
	value := os.Getenv(e.name)
	if value == "" {
		return "", fmt.Errorf("credential env var %s is empty", e.name)
	}
	return value, nil
}

func (f fileCredentialSource) Value() (string, error) {
	value, err := os.ReadFile(f.path)
	if err != nil {
		return "", fmt.Errorf("read credential file %s: %w", f.path, err)
	}
	if strings.TrimSpace(string(value)) == "" {
		return "", fmt.Errorf("credential file %s is empty", f.path)
	}
	return strings.TrimSpace(string(value)), nil
}
