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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type loggingUserSecretReader struct {
	reader client.Reader
}

// NewLoggingUserSecretReader wraps an uncached reader and logs every Get/List call.
func NewLoggingUserSecretReader(reader client.Reader) client.Reader {
	if reader == nil {
		return nil
	}
	return &loggingUserSecretReader{reader: reader}
}

func (r *loggingUserSecretReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	log.FromContext(ctx).Info("reading user-owned Secret",
		"operation", "Get",
		"type", fmt.Sprintf("%T", obj),
		"namespace", key.Namespace,
		"name", key.Name,
	)
	return r.reader.Get(ctx, key, obj, opts...)
}

func (r *loggingUserSecretReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	log.FromContext(ctx).Info("reading user-owned Secret",
		"operation", "List",
		"type", fmt.Sprintf("%T", list),
	)
	return r.reader.List(ctx, list, opts...)
}
