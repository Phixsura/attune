// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package guardpolicy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmguard"
)

func Test_marshalPolicyJSON_EmptyPolicy(t *testing.T) {
	t.Parallel()
	target, rules, err := marshalPolicyJSON(llmguard.Policy{})
	require.NoError(t, err)
	require.NotNil(t, target)
	require.NotNil(t, rules)
}

func Test_marshalPolicyJSON_WithTarget(t *testing.T) {
	t.Parallel()
	p := llmguard.Policy{
		Target: llmguard.Target{
			Channels: []string{"openai"},
		},
		Rules: []llmguard.Rule{
			{Guard: "pii", Action: llmguard.ActionBlock},
		},
	}
	target, rules, err := marshalPolicyJSON(p)
	require.NoError(t, err)

	var parsedTarget llmguard.Target
	require.NoError(t, json.Unmarshal(target, &parsedTarget))
	require.Equal(t, []string{"openai"}, parsedTarget.Channels)

	var parsedRules []llmguard.Rule
	require.NoError(t, json.Unmarshal(rules, &parsedRules))
	require.Len(t, parsedRules, 1)
	require.Equal(t, "pii", parsedRules[0].Guard)
}
