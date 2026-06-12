package models

type PermissionDescriptor struct {
	Name        string
	Description string
}

// PermissionDescriptors is the catalogue registered with iag-authentication at
// boot. Domain services that call the AI platform are granted ai.use_inference
// (usually via a service principal); ai.manage_prompts and ai.view_usage are
// platform-admin scoped.
func PermissionDescriptors() []PermissionDescriptor {
	return []PermissionDescriptor{
		{Name: "ai.use_inference", Description: "Run completions, embeddings, and saved prompts via the AI gateway"},
		{Name: "ai.manage_prompts", Description: "Create, update, and delete shared prompt templates"},
		{Name: "ai.view_usage", Description: "View AI usage and cost-attribution reports"},
		{Name: "ai.run_agents", Description: "Run AI agents and the multi-agent orchestrator"},
		{Name: "ai.manage_agents", Description: "Create, update, and delete agent definitions"},
	}
}
