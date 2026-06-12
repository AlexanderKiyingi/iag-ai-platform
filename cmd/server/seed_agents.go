package main

import (
	"context"
	"encoding/json"
	"log"

	"iag-ai-platform/backend/internal/repository"
	"iag-ai-platform/backend/internal/tools"
)

// parseServices turns AI_SERVICES_JSON into the callable-service catalog. An
// empty or invalid value yields an empty catalog (agents can still run; the
// call_microservice tool just reports nothing is configured).
func parseServices(raw string) map[string]tools.ServiceSpec {
	out := map[string]tools.ServiceSpec{}
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		log.Printf("ai-platform: AI_SERVICES_JSON invalid, ignoring: %v", err)
		return map[string]tools.ServiceSpec{}
	}
	return out
}

// seedAgents installs the default coordinator + specialist agents if they don't
// already exist. Operators can edit or add agents via the API afterwards; seeds
// never clobber existing definitions.
func seedAgents(ctx context.Context, repo *repository.Repository) {
	defaults := []repository.AgentInput{
		{
			Name:        coordinatorAgent,
			Description: "Multi-agent orchestrator. Decomposes a task and delegates to specialist agents.",
			Tools:       []string{"delegate", "list_services"},
			System: "You are the IAG AI orchestrator. Break the user's request into clear subtasks " +
				"and use the `delegate` tool to assign each to the most suitable specialist agent " +
				"(for example: finance-analyst, procurement-analyst, ops-analyst). Use `list_services` " +
				"to understand what backends are available. Once you have the specialists' answers, " +
				"synthesize a single, concise, well-structured response for the user. Do not invent " +
				"data — rely on what the specialists report.",
		},
		{
			Name:        "ops-analyst",
			Description: "General operations analyst. Answers questions by calling IAG microservices.",
			Tools:       []string{"list_services", "call_microservice"},
			System: "You are an IAG operations analyst. Answer the question using live data from the " +
				"platform's microservices. Call `list_services` to see what's available, then use " +
				"`call_microservice` (read-only) to fetch what you need. Cite the service and path you " +
				"used. If a needed service isn't available, say so plainly.",
		},
		{
			Name:        "finance-analyst",
			Description: "Finance specialist (GL, AR/AP, budgets, reconciliation).",
			Tools:       []string{"list_services", "call_microservice"},
			System: "You are an IAG finance specialist. Use `call_microservice` against the finance " +
				"service (audience iag.finance) to answer questions about the general ledger, AR/AP, " +
				"trial balance, reconciliation, and payroll postings. Be precise with figures and " +
				"name the endpoints you used. Never guess numbers.",
		},
		{
			Name:        "procurement-analyst",
			Description: "Procurement specialist (requisitions, RFQs, POs, vendors, budgets).",
			Tools:       []string{"list_services", "call_microservice"},
			System: "You are an IAG procurement specialist. Use `call_microservice` against the " +
				"procurement service (audience iag.procurement) to answer questions about " +
				"requisitions, RFQs, purchase orders, vendors, and budget commitments. Summarize " +
				"clearly and reference the data you fetched.",
		},
	}
	for _, a := range defaults {
		if err := repo.SeedAgent(ctx, a); err != nil {
			log.Printf("ai-platform: seed agent %s: %v", a.Name, err)
		}
	}
}
