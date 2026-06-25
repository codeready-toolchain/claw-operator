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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackBodyTokenReplacer_Rewrite(t *testing.T) {
	const (
		envVar    = "CRED_SLACK_BOT"
		realToken = "xoxb-real-bot-token"
	)

	rewriter := NewSlackBodyTokenReplacer(envVar)

	t.Run("JSON body with matching token field", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"token":"xoxb-placeholder","channel":"C1234"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		var obj map[string]any
		require.NoError(t, json.Unmarshal(result, &obj))
		assert.Equal(t, realToken, obj["token"])
		assert.Equal(t, "C1234", obj["channel"])
	})

	t.Run("JSON body with placeholder in message field is not replaced", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"token":"xoxb-placeholder","text":"hello xoxb-placeholder","channel":"C1234"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		var obj map[string]any
		require.NoError(t, json.Unmarshal(result, &obj))
		assert.Equal(t, realToken, obj["token"])
		assert.Equal(t, "hello xoxb-placeholder", obj["text"], "message field must not be modified")
	})

	t.Run("JSON body with no token field", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"channel":"C1234","text":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		assert.JSONEq(t, body, string(result))
	})

	t.Run("JSON body where token field has a different value", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"token":"some-other-token","channel":"C1234"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		var obj map[string]any
		require.NoError(t, json.Unmarshal(result, &obj))
		assert.Equal(t, "some-other-token", obj["token"], "should not replace non-matching token value")
	})

	t.Run("form-encoded body with matching token field", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := "token=xoxb-placeholder&channel=C1234"
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		values, err := url.ParseQuery(string(result))
		require.NoError(t, err)
		assert.Equal(t, realToken, values.Get("token"))
		assert.Equal(t, "C1234", values.Get("channel"))
	})

	t.Run("form-encoded body with placeholder in text field is not replaced", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := "token=xoxb-placeholder&text=hello+xoxb-placeholder&channel=C1234"
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		values, err := url.ParseQuery(string(result))
		require.NoError(t, err)
		assert.Equal(t, realToken, values.Get("token"))
		assert.Equal(t, "hello xoxb-placeholder", values.Get("text"),
			"text field must not be modified")
	})

	t.Run("nil body is a no-op", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		req := httptest.NewRequest(http.MethodGet, "https://slack.com/api/auth.test", nil)
		req.Body = nil

		require.NoError(t, rewriter.Rewrite(req))
	})

	t.Run("empty body is a no-op", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/auth.test",
			bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		assert.Empty(t, result)
	})

	t.Run("unknown content type leaves body unchanged", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `token=xoxb-placeholder`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "text/plain")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		assert.Equal(t, body, string(result))
	})

	t.Run("content type with charset parameter", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"token":"xoxb-placeholder","channel":"C1234"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		var obj map[string]any
		require.NoError(t, json.Unmarshal(result, &obj))
		assert.Equal(t, realToken, obj["token"])
	})

	t.Run("empty env var returns error", func(t *testing.T) {
		t.Setenv(envVar, "")
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(`{"token":"xoxb-placeholder"}`))
		req.Header.Set("Content-Type", "application/json")

		err := rewriter.Rewrite(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), envVar)
	})

	t.Run("updates ContentLength after JSON replacement", func(t *testing.T) {
		t.Setenv(envVar, realToken)
		body := `{"token":"xoxb-placeholder"}`
		req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		require.NoError(t, rewriter.Rewrite(req))

		result, _ := io.ReadAll(req.Body)
		assert.Equal(t, int64(len(result)), req.ContentLength)
	})
}
