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
	"net/http"
	"strings"
)

// BearerInjector injects Authorization: Bearer <token>.
type BearerInjector struct {
	source         credentialSource
	defaultHeaders map[string]string
}

func NewBearerInjector(route *Route) (*BearerInjector, error) {
	source, err := newCredentialSource(route, "bearer")
	if err != nil {
		return nil, err
	}
	return &BearerInjector{
		source:         source,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (b *BearerInjector) Inject(req *http.Request) error {
	token, err := b.source.Value()
	if err != nil {
		return err
	}
	for k, v := range b.defaultHeaders {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}
