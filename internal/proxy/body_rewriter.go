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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
)

// BodyRewriter replaces token placeholder values in request bodies.
type BodyRewriter interface {
	Rewrite(req *http.Request) error
}

const slackBotPlaceholder = "xoxb-placeholder"

// SlackBodyTokenReplacer replaces the "token" field in JSON and
// form-encoded request bodies sent to Slack. Only the root-level
// "token" field whose value matches "xoxb-placeholder" is replaced;
// other fields are left untouched.
type SlackBodyTokenReplacer struct {
	envVar string
}

func NewSlackBodyTokenReplacer(envVar string) *SlackBodyTokenReplacer {
	return &SlackBodyTokenReplacer{envVar: envVar}
}

func (s *SlackBodyTokenReplacer) Rewrite(req *http.Request) error {
	token := os.Getenv(s.envVar)
	if token == "" {
		return fmt.Errorf("credential env var %s is empty", s.envVar)
	}

	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}

	if len(body) == 0 {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	mediaType := ""
	if ct := req.Header.Get("Content-Type"); ct != "" {
		mediaType, _, _ = mime.ParseMediaType(ct)
	}

	var replaced []byte
	switch mediaType {
	case "application/json":
		replaced, err = rewriteSlackJSON(body, token)
		if err != nil {
			return err
		}
	case "application/x-www-form-urlencoded":
		replaced = rewriteSlackForm(body, token)
	default:
		req.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	req.Body = io.NopCloser(bytes.NewReader(replaced))
	req.ContentLength = int64(len(replaced))
	return nil
}

func rewriteSlackJSON(body []byte, token string) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, nil
	}

	val, ok := obj["token"]
	if !ok {
		return body, nil
	}
	str, ok := val.(string)
	if !ok || str != slackBotPlaceholder {
		return body, nil
	}

	obj["token"] = token
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshal rewritten JSON body: %w", err)
	}
	return out, nil
}

func rewriteSlackForm(body []byte, token string) []byte {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return body
	}

	if values.Get("token") != slackBotPlaceholder {
		return body
	}

	values.Set("token", token)
	return []byte(values.Encode())
}
