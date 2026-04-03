package article

import (
	"context"

	"github.com/bornholm/genai/agent"
)

const (
	ContextKeyDocumentPlan          agent.ContextKey = "article_document_plan"
	ContextKeySubject               agent.ContextKey = "article_subject"
	ContextKeyTargetWordCount       agent.ContextKey = "article_target_word_count"
	ContextKeyResearchDepth         agent.ContextKey = "article_research_depth"
	ContextKeyAgentRole             agent.ContextKey = "article_agent_role"
	ContextKeyStyleGuidelines       agent.ContextKey = "article_style_guidelines"
	ContextKeyAdditionalContext     agent.ContextKey = "article_additional_context"
	ContextKeyKnowledgeBase         agent.ContextKey = "article_knowledge_base"
	ContextKeyResearchComplete      agent.ContextKey = "article_research_complete"
	ContextKeySourceAttribution     agent.ContextKey = "article_source_attribution"
	ContextKeyDocumentSection       agent.ContextKey = "article_document_section"
	ContextKeyPreviousSectionContent agent.ContextKey = "article_previous_section_content"
	ContextKeyDocumentSections      agent.ContextKey = "article_document_sections"
	ContextKeyDocumentDraft         agent.ContextKey = "article_document_draft"
)

// AgentRole defines the role of an agent in the article writing process
type AgentRole string

const (
	RoleResearcher   AgentRole = "researcher" // NEW
	RolePlanner      AgentRole = "planner"
	RoleWriter       AgentRole = "writer"
	RoleEditor       AgentRole = "editor"
	RoleOrchestrator AgentRole = "orchestrator"
)

// ResearchDepth defines how deep the research should be
type ResearchDepth string

const (
	ResearchBasic    ResearchDepth = "basic"
	ResearchDeep     ResearchDepth = "deep"
	ResearchDeepWeb  ResearchDepth = "deep_web"
	ResearchAcademic ResearchDepth = "academic"
)

// WithContextDocumentPlan adds a document plan to the context
func WithContextDocumentPlan(ctx context.Context, plan DocumentPlan) context.Context {
	return context.WithValue(ctx, ContextKeyDocumentPlan, plan)
}

// ContextDocumentPlan retrieves the document plan from context
func ContextDocumentPlan(ctx context.Context) (DocumentPlan, bool) {
	plan, ok := ctx.Value(ContextKeyDocumentPlan).(DocumentPlan)
	return plan, ok
}

// WithContextSubject adds the article subject to the context
func WithContextSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, ContextKeySubject, subject)
}

// ContextSubject retrieves the article subject from context
func ContextSubject(ctx context.Context, defaultSubject string) string {
	return agent.ContextValue(ctx, ContextKeySubject, defaultSubject)
}

// WithContextTargetWordCount adds target word count to the context
func WithContextTargetWordCount(ctx context.Context, wordCount int) context.Context {
	return context.WithValue(ctx, ContextKeyTargetWordCount, wordCount)
}

// ContextTargetWordCount retrieves the target word count from context
func ContextTargetWordCount(ctx context.Context, defaultWordCount int) int {
	return agent.ContextValue(ctx, ContextKeyTargetWordCount, defaultWordCount)
}

// WithContextResearchDepth adds research depth to the context
func WithContextResearchDepth(ctx context.Context, depth ResearchDepth) context.Context {
	return context.WithValue(ctx, ContextKeyResearchDepth, depth)
}

// ContextResearchDepth retrieves the research depth from context
func ContextResearchDepth(ctx context.Context, defaultDepth ResearchDepth) ResearchDepth {
	return agent.ContextValue(ctx, ContextKeyResearchDepth, defaultDepth)
}

// WithContextAgentRole adds agent role to the context
func WithContextAgentRole(ctx context.Context, role AgentRole) context.Context {
	return context.WithValue(ctx, ContextKeyAgentRole, role)
}

// ContextAgentRole retrieves the agent role from context
func ContextAgentRole(ctx context.Context, defaultRole AgentRole) AgentRole {
	return agent.ContextValue(ctx, ContextKeyAgentRole, defaultRole)
}

// WithContextStyleGuidelines adds style guidelines to the context
func WithContextStyleGuidelines(ctx context.Context, styleGuidelines string) context.Context {
	return context.WithValue(ctx, ContextKeyStyleGuidelines, styleGuidelines)
}

// ContextStyleGuidelines retrieves the style guidelines from context
func ContextStyleGuidelines(ctx context.Context, defaultGuidelines string) string {
	return agent.ContextValue(ctx, ContextKeyStyleGuidelines, defaultGuidelines)
}

// WithContextAdditionalContext adds additional context information to the context
func WithContextAdditionalContext(ctx context.Context, additionalContext string) context.Context {
	return context.WithValue(ctx, ContextKeyAdditionalContext, additionalContext)
}

// ContextAdditionalContext retrieves the additional context information from context
func ContextAdditionalContext(ctx context.Context, defaultContext string) string {
	return agent.ContextValue(ctx, ContextKeyAdditionalContext, defaultContext)
}

// WithContextKnowledgeBase adds knowledge base to context
func WithContextKnowledgeBase(ctx context.Context, kb KnowledgeBase) context.Context {
	return context.WithValue(ctx, ContextKeyKnowledgeBase, kb)
}

// ContextKnowledgeBase retrieves knowledge base from context
func ContextKnowledgeBase(ctx context.Context) (KnowledgeBase, bool) {
	kb, ok := ctx.Value(ContextKeyKnowledgeBase).(KnowledgeBase)
	return kb, ok
}

// WithContextResearchComplete marks research as complete in context
func WithContextResearchComplete(ctx context.Context, complete bool) context.Context {
	return context.WithValue(ctx, ContextKeyResearchComplete, complete)
}

// ContextResearchComplete checks if research is complete from context
func ContextResearchComplete(ctx context.Context) bool {
	complete, ok := ctx.Value(ContextKeyResearchComplete).(bool)
	return ok && complete
}

// WithContextDocumentSection adds the current document section to context
func WithContextDocumentSection(ctx context.Context, section DocumentSection) context.Context {
	return context.WithValue(ctx, ContextKeyDocumentSection, section)
}

// ContextDocumentSection retrieves the current document section from context
func ContextDocumentSection(ctx context.Context) (DocumentSection, bool) {
	section, ok := ctx.Value(ContextKeyDocumentSection).(DocumentSection)
	return section, ok
}

// WithContextPreviousSectionContent adds the previous section content to context
func WithContextPreviousSectionContent(ctx context.Context, section *SectionContent) context.Context {
	return context.WithValue(ctx, ContextKeyPreviousSectionContent, section)
}

// ContextPreviousSectionContent retrieves the previous section content from context
func ContextPreviousSectionContent(ctx context.Context) *SectionContent {
	section, _ := ctx.Value(ContextKeyPreviousSectionContent).(*SectionContent)
	return section
}

// WithContextDocumentSections adds all document sections to context
func WithContextDocumentSections(ctx context.Context, sections []SectionContent) context.Context {
	return context.WithValue(ctx, ContextKeyDocumentSections, sections)
}

// ContextDocumentSections retrieves all document sections from context
func ContextDocumentSections(ctx context.Context) ([]SectionContent, bool) {
	sections, ok := ctx.Value(ContextKeyDocumentSections).([]SectionContent)
	return sections, ok
}

// WithContextDocumentDraft adds the assembled markdown draft to context (sections validated so far)
func WithContextDocumentDraft(ctx context.Context, draft string) context.Context {
	return context.WithValue(ctx, ContextKeyDocumentDraft, draft)
}

// ContextDocumentDraft retrieves the assembled markdown draft from context
func ContextDocumentDraft(ctx context.Context) string {
	draft, _ := ctx.Value(ContextKeyDocumentDraft).(string)
	return draft
}
