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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVertexGoogleAuthProxyStubScoped verifies the NODE_OPTIONS preload stubs only
// UserRefreshClient instances minted from the operator stub ADC, leaving other
// google-auth consumers (e.g. a second provider using real refresh credentials)
// on the normal auth path without UserRefreshClient.prototype mutation.
//
// REVERT: delete this test with vertexGoogleAuthProxyStubJS once
// openclaw/openclaw#108350 is in a published anthropic-vertex-provider.
func TestVertexGoogleAuthProxyStubScoped(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping google-auth proxy stub tests")
	}

	tmpDir := t.TempDir()
	stubPath := filepath.Join(tmpDir, "google-auth-proxy-stub.js")
	require.NoError(t, os.WriteFile(stubPath, []byte(vertexGoogleAuthProxyStubJS), 0o644))

	galDir := filepath.Join(tmpDir, "node_modules", "google-auth-library")
	require.NoError(t, os.MkdirAll(galDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(galDir, "package.json"), []byte(`{"name":"google-auth-library","main":"index.js"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(galDir, "index.js"), []byte(`'use strict';
function UserRefreshClient() {
  this.credentials = {};
  this.setCredentials = function (creds) { this.credentials = Object.assign({}, creds); };
  this.getRequestHeaders = async function () {
    const token = (this.credentials && this.credentials.access_token) || 'missing';
    return { Authorization: 'Bearer ' + token };
  };
  this.refreshAccessToken = async function () {
    throw new Error('network refresh should not run for stub ADC');
  };
  this.refreshTokenNoCache = async function () {
    throw new Error('network refresh should not run for stub ADC');
  };
}
function GoogleAuth(opts) {
  this._profile = (opts && opts.profile) || 'other';
}
GoogleAuth.prototype.getClient = async function () {
  const client = new UserRefreshClient();
  if (this._profile === 'vertex-adc') {
    client._clientId = 'stub.apps.googleusercontent.com';
    client.credentials = { refresh_token: 'proxy-managed-token' };
    return client;
  }
  client._clientId = 'real.apps.googleusercontent.com';
  client.credentials = { refresh_token: 'real-user-refresh', access_token: 'real-access-token' };
  client.refreshAccessToken = async function () {
    this.credentials.access_token = 'real-access-token-refreshed';
    return { credentials: this.credentials };
  };
  return client;
};
module.exports = { GoogleAuth, UserRefreshClient };
`), 0o644))

	harnessPath := filepath.Join(tmpDir, "harness.js")
	require.NoError(t, os.WriteFile(harnessPath, []byte(`'use strict';
(async () => {
  process.env.OPENCLAW_PROXY_ACTIVE = '1';
  require('./google-auth-proxy-stub.js');
  const gal = require('google-auth-library');
  const marker = Symbol.for('claw.vertexProxyADC');

  const vertexAuth = new gal.GoogleAuth({ profile: 'vertex-adc' });
  const otherAuth = new gal.GoogleAuth({ profile: 'other' });
  const vertexClient = await vertexAuth.getClient();
  const otherClient = await otherAuth.getClient();

  const vertexRefresh = await vertexClient.refreshAccessToken();
  const otherRefresh = await otherClient.refreshAccessToken();
  const vertexHeaders = await vertexClient.getRequestHeaders();
  const otherHeaders = await otherClient.getRequestHeaders();

  const protoHasStub = typeof gal.UserRefreshClient.prototype.refreshAccessToken === 'function' &&
    gal.UserRefreshClient.prototype.refreshAccessToken.toString().includes('claw-proxy-vended-token');

  console.log(JSON.stringify({
    vertexMarked: !!vertexClient[marker],
    otherMarked: !!otherClient[marker],
    vertexToken: vertexRefresh.credentials.access_token,
    otherToken: otherRefresh.credentials.access_token,
    vertexAuthz: vertexHeaders.Authorization,
    otherAuthz: otherHeaders.Authorization,
    protoMutated: protoHasStub,
  }));
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
`), 0o644))

	cmd := exec.Command("node", harnessPath) //nolint:gosec
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "OPENCLAW_PROXY_ACTIVE=1", "NODE_PATH="+filepath.Join(tmpDir, "node_modules"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "harness failed: %s", string(out))

	var result struct {
		VertexMarked bool   `json:"vertexMarked"`
		OtherMarked  bool   `json:"otherMarked"`
		VertexToken  string `json:"vertexToken"`
		OtherToken   string `json:"otherToken"`
		VertexAuthz  string `json:"vertexAuthz"`
		OtherAuthz   string `json:"otherAuthz"`
		ProtoMutated bool   `json:"protoMutated"`
	}
	require.NoError(t, json.Unmarshal(out, &result), "harness output: %s", string(out))

	assert.True(t, result.VertexMarked, "Vertex stub ADC client should carry the proxy marker")
	assert.False(t, result.OtherMarked, "non-stub GoogleAuth client must not be marked")
	assert.Equal(t, vertexProxyVendedToken, result.VertexToken)
	assert.Equal(t, "real-access-token-refreshed", result.OtherToken)
	assert.Equal(t, "Bearer "+vertexProxyVendedToken, result.VertexAuthz)
	assert.Equal(t, "Bearer real-access-token-refreshed", result.OtherAuthz)
	assert.False(t, result.ProtoMutated, "UserRefreshClient.prototype must remain unstubbed")
}
