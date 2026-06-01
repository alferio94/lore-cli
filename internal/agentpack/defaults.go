package agentpack

func DefaultDefinition() Definition {
	return DefaultOperationalAssets().Definition()
}

func DefaultOperationalAssets() OperationalAssets {
	return OperationalAssets{
		PackID:   "portable-agent-pack",
		Persona:  defaultPersona(),
		Workflow: defaultWorkflow(),
		Roles:    defaultRoles(),
		Profiles: defaultProfiles(),
		Agents:   defaultManagedAgentAssets(),
	}
}

func defaultPersona() Persona {
	return Persona{
		Name:            "Lore",
		Identity:        "Calm technical partner for Lore workflows.",
		Tone:            "Low-energy, precise, slightly witty.",
		LanguagePolicy:  "Spanish input receives neutral Mexican Spanish; persisted technical artifacts stay in English.",
		BehaviorRules:   []string{"Verify technical claims before agreeing.", "Challenge risky shortcuts with evidence.", "Keep secrets out of generated config and logs."},
		MentorTriggers:  []string{"architectural decisions", "dangerous shortcuts", "conceptual mistakes"},
		WorkerExecution: "Repository-heavy work happens in focused workers; in SDD mode, delegate each phase to its matching sdd-* worker when delegation is available. Phase workers persist full artifacts and return compact envelopes; the orchestrator must not author phase artifacts as a shortcut.",
	}
}

func defaultWorkflow() Workflow {
	return Workflow{Phases: []Phase{
		{ID: PhaseInit, Title: "Init", Summary: "Initialize SDD context and detect project conventions."},
		{ID: PhaseExplore, Title: "Explore", Summary: "Investigate the current codebase and constraints."},
		{ID: PhaseProposal, Title: "Proposal", Summary: "Define intent, scope, risks, and rollback."},
		{ID: PhaseSpec, Title: "Spec", Summary: "Write requirements and acceptance scenarios."},
		{ID: PhaseDesign, Title: "Design", Summary: "Describe the technical approach and interfaces."},
		{ID: PhaseTasks, Title: "Tasks", Summary: "Break the change into bounded implementation slices."},
		{ID: PhaseApply, Title: "Apply", Summary: "Implement one bounded slice and persist progress."},
		{ID: PhaseVerify, Title: "Verify", Summary: "Validate implementation against specs and design."},
		{ID: PhaseArchive, Title: "Archive", Summary: "Record the completed change and final traceability."},
	}}
}

func defaultRoles() []Role {
	return []Role{
		{Name: RoleOrchestrator, Kind: "orchestrator", Summary: "Owns decisions, pacing, user-facing synthesis, and SDD phase delegation rather than phase artifact authoring."},
		{Name: RoleLoreWorker, Kind: "worker", Summary: "Canonical repository worker for non-SDD execution."},
		{Name: "sdd-init", Kind: "phase", Summary: "Initializes SDD context."},
		{Name: "sdd-explore", Kind: "phase", Summary: "Explores requirements and current state."},
		{Name: "sdd-propose", Kind: "phase", Summary: "Prepares the change proposal."},
		{Name: "sdd-spec", Kind: "phase", Summary: "Writes delta specs."},
		{Name: "sdd-design", Kind: "phase", Summary: "Writes the technical design."},
		{Name: "sdd-tasks", Kind: "phase", Summary: "Builds the implementation checklist."},
		{Name: "sdd-apply", Kind: "phase", Summary: "Implements bounded slices with checkpoints."},
		{Name: "sdd-verify", Kind: "phase", Summary: "Verifies code and artifacts."},
		{Name: "sdd-archive", Kind: "phase", Summary: "Archives completed changes."},
	}
}

func defaultProfiles() []Profile {
	return []Profile{
		{
			ID:           ProfileBalanced,
			Description:  "Default portable profile for daily Lore work.",
			DefaultModel: ModelGPT5,
			RoleModels: map[string]string{
				RoleOrchestrator: ModelGPT5,
				RoleLoreWorker:   ModelGPT5Mini,
				"sdd-apply":      ModelGPT5,
				"sdd-verify":     ModelGPT5,
			},
		},
		{
			ID:           ProfileSpeed,
			Description:  "Lower-cost profile for broad inspection and routine slices.",
			DefaultModel: ModelGPT5Mini,
			RoleModels: map[string]string{
				RoleOrchestrator: ModelGPT5Mini,
				RoleLoreWorker:   ModelGPT5Mini,
				"sdd-verify":     ModelGPT5,
			},
		},
		{
			ID:           ProfileCodex,
			Description:  "Codex harness profile using gpt-5.4 as the default model.",
			DefaultModel: ModelGPT54,
			RoleModels: map[string]string{
				RoleOrchestrator: ModelGPT54,
				RoleLoreWorker:   ModelGPT54,
				"sdd-init":       ModelGPT54,
				"sdd-explore":    ModelGPT54,
				"sdd-propose":    ModelGPT54,
				"sdd-spec":       ModelGPT54,
				"sdd-design":     ModelGPT54,
				"sdd-tasks":      ModelGPT54,
				"sdd-apply":      ModelGPT54,
				"sdd-verify":     ModelGPT54,
				"sdd-archive":    ModelGPT54,
			},
		},
	}
}
