package agent

import (
	"context"
	"strings"
	"time"

	"denova/config"
)

const (
	AgentKindUnknown             = "unknown"
	AgentKindIDE                 = "ide"
	AgentKindInteractiveStory    = "interactive_story"
	AgentKindInteractiveDirector = "interactive_director"
	AgentKindConfigManager       = "config_manager"
	AgentKindImage               = "image"
	AgentKindAutomation          = "automation"
)

// RunOptions identifies one Agent run across runtime, trace, and UI surfaces.
type RunOptions struct {
	AgentKind              string
	RootAgentName          string
	ModelIdentities        map[string]RunModelIdentity
	TaskID                 string
	SessionID              string
	ReviewThreadID         string
	StoryID                string
	BranchID               string
	TurnID                 string
	MaintenanceTask        string
	Workspace              string
	Mode                   string
	IdleTimeout            time.Duration
	ToolResultMaxBytes     int
	SystemPromptLog        SystemPromptCompositionLog
	OnMutationsVerified    func(context.Context, []ToolMutation, PostRunVerification)
	OnUserMessageCommitted func(context.Context) error
}

// RunModelIdentity is display-only metadata for the resolved model used by one
// root Agent or configured SubAgent. It never enters the model-visible context.
type RunModelIdentity struct {
	ProfileID string
	ModelName string
}

// ResolveRunModelIdentities snapshots the exact model resolution used to build
// a run. Keying by ADK AgentName keeps root and SubAgent stream events aligned
// with the same config snapshot even if settings change while the run is active.
func ResolveRunModelIdentities(cfg *config.Config, parentKind string) map[string]RunModelIdentity {
	if cfg == nil {
		return nil
	}
	identities := map[string]RunModelIdentity{}
	if rootName := rootAgentNameForKind(parentKind); rootName != "" {
		identities[rootName] = runModelIdentity(config.ResolveAgentModel(cfg, parentKind))
	}
	if !config.IsDeepAgentParentKind(parentKind) {
		return identities
	}
	for _, sub := range config.SanitizeSubAgents(cfg.SubAgents) {
		if !config.SubAgentAllowedForParent(sub, parentKind) {
			continue
		}
		identities[sub.ID] = runModelIdentity(config.ResolveSubAgentModel(cfg, parentKind, sub))
	}
	return identities
}

func runModelIdentity(resolved config.ResolvedModelSettings) RunModelIdentity {
	return RunModelIdentity{
		ProfileID: strings.TrimSpace(resolved.ProfileID),
		ModelName: strings.TrimSpace(resolved.OpenAIModel),
	}
}

func (o RunOptions) normalized(defaultWorkspace string) RunOptions {
	o.AgentKind = strings.TrimSpace(o.AgentKind)
	if o.AgentKind == "" {
		o.AgentKind = AgentKindUnknown
	}
	o.RootAgentName = strings.TrimSpace(o.RootAgentName)
	if o.RootAgentName == "" {
		o.RootAgentName = rootAgentNameForKind(o.AgentKind)
	}
	if len(o.ModelIdentities) > 0 {
		identities := make(map[string]RunModelIdentity, len(o.ModelIdentities))
		for agentName, identity := range o.ModelIdentities {
			agentName = strings.TrimSpace(agentName)
			identity.ProfileID = strings.TrimSpace(identity.ProfileID)
			identity.ModelName = strings.TrimSpace(identity.ModelName)
			if agentName == "" || (identity.ProfileID == "" && identity.ModelName == "") {
				continue
			}
			identities[agentName] = identity
		}
		o.ModelIdentities = identities
	}
	o.TaskID = strings.TrimSpace(o.TaskID)
	o.SessionID = strings.TrimSpace(o.SessionID)
	o.ReviewThreadID = strings.TrimSpace(o.ReviewThreadID)
	o.StoryID = strings.TrimSpace(o.StoryID)
	o.BranchID = strings.TrimSpace(o.BranchID)
	o.TurnID = strings.TrimSpace(o.TurnID)
	o.MaintenanceTask = strings.TrimSpace(o.MaintenanceTask)
	o.Workspace = strings.TrimSpace(o.Workspace)
	if o.Workspace == "" {
		o.Workspace = strings.TrimSpace(defaultWorkspace)
	}
	o.Mode = strings.TrimSpace(o.Mode)
	if o.IdleTimeout < 0 {
		o.IdleTimeout = 0
	}
	if o.ToolResultMaxBytes < 0 {
		o.ToolResultMaxBytes = 0
	}
	return o
}

func (o RunOptions) modelIdentity(agentName string) RunModelIdentity {
	if len(o.ModelIdentities) == 0 {
		return RunModelIdentity{}
	}
	if identity, ok := o.ModelIdentities[strings.TrimSpace(agentName)]; ok {
		return identity
	}
	return o.ModelIdentities[o.RootAgentName]
}

func rootAgentNameForKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case AgentKindIDE:
		return "DenovaAgent"
	case AgentKindInteractiveStory:
		return "DenovaInteractiveStoryAgent"
	case AgentKindInteractiveDirector:
		return "DenovaInteractiveDirectorAgent"
	case AgentKindConfigManager:
		return "DenovaConfigManagerAgent"
	case AgentKindImage:
		return "DenovaImageAgent"
	case AgentKindAutomation:
		return "DenovaAutomationAgent"
	default:
		return ""
	}
}

func (o RunOptions) checkpointID(runID string) string {
	parts := []string{strings.TrimSpace(o.AgentKind)}
	switch {
	case strings.TrimSpace(o.SessionID) != "":
		parts = append(parts, "session", strings.TrimSpace(o.SessionID))
	case strings.TrimSpace(o.TaskID) != "":
		parts = append(parts, "task", strings.TrimSpace(o.TaskID))
	case strings.TrimSpace(runID) != "":
		parts = append(parts, "run", strings.TrimSpace(runID))
	default:
		return ""
	}
	return strings.Join(parts, ":")
}

const runTraceMetadataValueMaxBytes = 256

func runTraceMetadataForConversation(options RunOptions, conversation Conversation) RunTraceMetadata {
	metadata := RunTraceMetadata{
		StoryID:         options.StoryID,
		BranchID:        options.BranchID,
		TurnID:          options.TurnID,
		MaintenanceTask: options.MaintenanceTask,
	}
	if reporter, ok := conversation.(RunTraceMetadataReporter); ok {
		reported := reporter.RunTraceMetadata()
		if strings.TrimSpace(reported.StoryID) != "" {
			metadata.StoryID = reported.StoryID
		}
		if strings.TrimSpace(reported.BranchID) != "" {
			metadata.BranchID = reported.BranchID
		}
		if strings.TrimSpace(reported.TurnID) != "" {
			metadata.TurnID = reported.TurnID
		}
		if strings.TrimSpace(reported.MaintenanceTask) != "" {
			metadata.MaintenanceTask = reported.MaintenanceTask
		}
	}
	metadata.StoryID = boundedRunTraceMetadataValue(metadata.StoryID)
	metadata.BranchID = boundedRunTraceMetadataValue(metadata.BranchID)
	metadata.TurnID = boundedRunTraceMetadataValue(metadata.TurnID)
	metadata.MaintenanceTask = boundedRunTraceMetadataValue(metadata.MaintenanceTask)
	return metadata
}

func boundedRunTraceMetadataValue(value string) string {
	return truncateUTF8StringBytes(strings.TrimSpace(value), runTraceMetadataValueMaxBytes)
}

func (m RunTraceMetadata) empty() bool {
	return m.StoryID == "" && m.BranchID == "" && m.TurnID == "" && m.MaintenanceTask == ""
}

func (m RunTraceMetadata) record() map[string]any {
	return map[string]any{
		"story_id":         m.StoryID,
		"branch_id":        m.BranchID,
		"turn_id":          m.TurnID,
		"maintenance_task": m.MaintenanceTask,
	}
}
