package types

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinAgentYAMLDeclaresVideoEvidenceCapabilityForVideoAgents(t *testing.T) {
	configPath := filepath.Join("..", "..", "config", "builtin_agents.yaml")
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var file builtinAgentsFile
	require.NoError(t, yaml.Unmarshal(raw, &file))

	byID := make(map[string]BuiltinAgentEntry, len(file.BuiltinAgents))
	for _, entry := range file.BuiltinAgents {
		byID[entry.ID] = entry
	}

	require.Equal(t, "v1", byID[BuiltinQuickAnswerID].Config.VideoEvidenceCitation)
	require.Equal(t, "v1", byID[BuiltinSmartReasoningID].Config.VideoEvidenceCitation)
	require.Equal(t, "selected", byID[BuiltinSmartReasoningID].Config.SkillsSelectionMode)
	require.Contains(t, byID[BuiltinSmartReasoningID].Config.SelectedSkills, "retrieve-from-timeline")
	require.Empty(t, byID[BuiltinDataAnalystID].Config.VideoEvidenceCitation)
}
