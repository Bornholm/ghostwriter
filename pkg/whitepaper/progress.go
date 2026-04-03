package whitepaper

import "github.com/bornholm/ghostwriter/pkg/article"

// Phase constants for white paper generation.
// Reuses article.ProgressPhase and article.ProgressTracker.

const (
	PhaseInitializing = article.PhaseInitializing
	PhaseResearching  = article.PhaseResearching
	PhasePlanning     = article.PhasePlanning
	PhaseWriting      = article.PhaseWriting
	PhaseEditing      = article.PhaseEditing
	PhaseCoherence    article.ProgressPhase = "coherence"
	PhaseAssembling   article.ProgressPhase = "assembling"
	PhaseRendering    article.ProgressPhase = "rendering"
	PhaseCompleted    = article.PhaseCompleted
)

// Phase weights for overall progress calculation.
const (
	weightResearching = 0.10
	weightPlanning    = 0.10
	weightWriting     = 0.45
	weightEditing     = 0.20
	weightCoherence   = 0.10
	weightAssembling  = 0.05
)

// baseProgress returns the cumulative base progress at the start of each phase.
func baseProgress(phase article.ProgressPhase) float64 {
	switch phase {
	case PhaseResearching:
		return 0.0
	case PhasePlanning:
		return weightResearching
	case PhaseWriting:
		return weightResearching + weightPlanning
	case PhaseEditing:
		return weightResearching + weightPlanning + weightWriting
	case PhaseCoherence:
		return weightResearching + weightPlanning + weightWriting + weightEditing
	case PhaseAssembling:
		return weightResearching + weightPlanning + weightWriting + weightEditing + weightCoherence
	case PhaseCompleted:
		return 1.0
	default:
		return 0.0
	}
}
