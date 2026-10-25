package services

import "encoding/json"

// Missing inheritance switches use the workspace defaults. Explicit false is
// a local configuration choice and is always preserved when saving/exporting.
func (c *agentConfig) UnmarshalJSON(data []byte) error {
	type plain agentConfig
	next := plain{InheritModel: true, InheritGit: true}
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	*c = agentConfig(next)
	return nil
}

func (e *agentExportJSON) UnmarshalJSON(data []byte) error {
	if err := RejectEmojiIdentity(data); err != nil {
		return err
	}
	if err := RejectUnknownIconFields(data); err != nil {
		return err
	}
	type plain agentExportJSON
	next := plain{InheritModel: true, InheritGit: true}
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if err := ValidateAgentIcon(next.Icon); err != nil {
		return err
	}
	*e = agentExportJSON(next)
	return nil
}
