// Copyright © 2020 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package output_test

import (
	"testing"

	"github.com/kube-logging/logging-operator/pkg/sdk/logging/model/output"
	"github.com/kube-logging/logging-operator/pkg/sdk/logging/model/render"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestHTTP(t *testing.T) {
	CONFIG := []byte(`
compress: gzip
endpoint: http://logserver.com:9000/api
headers_from_placeholders: {"x-foo-bar":"${$.foo.bar}","x-tag":"app-${tag}"}
retryable_response_codes: [503,504]
reuse_connections: true
format:
  type: json
buffer:
  timekey: 1m
  timekey_wait: 30s
  timekey_use_utc: true
auth:
  username:
    value: admin
  password:
    value: pass
`)

	expected := `
  <match **>
    @type http
    @id test
    compress gzip
    endpoint http://logserver.com:9000/api
    headers_from_placeholders {"x-foo-bar":"${$.foo.bar}","x-tag":"app-${tag}"}
    retryable_response_codes [503,504]
    reuse_connections true
    <buffer tag,time>
      @type file
      path /buffers/test.*.buffer
      retry_forever true
      timekey 1m
      timekey_use_utc true
      timekey_wait 30s
    </buffer>
    <format>
      @type json
    </format>
	<auth>
      password pass
      username admin
	</auth>
  </match>
`

	http := &output.HTTPOutputConfig{}
	require.NoError(t, yaml.Unmarshal(CONFIG, http))
	test := render.NewOutputPluginTest(t, http)
	test.DiffResult(expected)
}
