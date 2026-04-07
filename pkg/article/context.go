package article

import (
	"context"

	"github.com/bornholm/genai/agent"
)

const (
	ContextKeySubject           agent.ContextKey = "article_subject"
	ContextKeyTargetWordCount   agent.ContextKey = "article_target_word_count"
	ContextKeyResearchDepth     agent.ContextKey = "article_research_depth"
	ContextKeyAgentRole         agent.ContextKey = "article_agent_role"
	ContextKeyStyleGuidelines   agent.ContextKey = "article_style_guidelines"
	ContextKeyAdditionalContext agent.ContextKey = "article_additional_context"
	ContextKeyKnowledgeBase     agent.ContextKey = "article_knowledge_base"
)

// AgentRole defines the role of an agent in the article writing process
type AgentRole string

const (
	RoleResearcher AgentRole = "researcher"
)

// ResearchDepth defines how deep the research should be
type ResearchDepth string

const (
	ResearchBasic    ResearchDepth = "basic"
	ResearchDeep     ResearchDepth = "deep"
	ResearchDeepWeb  ResearchDepth = "deep_web"
	ResearchAcademic ResearchDepth = "academic"
)

func WithContextSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, ContextKeySubject, subject)
}

func ContextSubject(ctx context.Context, defaultSubject string) string {
	return agent.ContextValue(ctx, ContextKeySubject, defaultSubject)
}

func WithContextTargetWordCount(ctx context.Context, wordCount int) context.Context {
	return context.WithValue(ctx, ContextKeyTargetWordCount, wordCount)
}

func ContextTargetWordCount(ctx context.Context, defaultWordCount int) int {
	return agent.ContextValue(ctx, ContextKeyTargetWordCount, defaultWordCount)
}

func WithContextResearchDepth(ctx context.Context, depth ResearchDepth) context.Context {
	return context.WithValue(ctx, ContextKeyResearchDepth, depth)
}

func ContextResearchDepth(ctx context.Context, defaultDepth ResearchDepth) ResearchDepth {
	return agent.ContextValue(ctx, ContextKeyResearchDepth, defaultDepth)
}

func WithContextAgentRole(ctx context.Context, role AgentRole) context.Context {
	return context.WithValue(ctx, ContextKeyAgentRole, role)
}

func ContextAgentRole(ctx context.Context, defaultRole AgentRole) AgentRole {
	return agent.ContextValue(ctx, ContextKeyAgentRole, defaultRole)
}

func WithContextStyleGuidelines(ctx context.Context, styleGuidelines string) context.Context {
	return context.WithValue(ctx, ContextKeyStyleGuidelines, styleGuidelines)
}

func ContextStyleGuidelines(ctx context.Context, defaultGuidelines string) string {
	return agent.ContextValue(ctx, ContextKeyStyleGuidelines, defaultGuidelines)
}

func WithContextAdditionalContext(ctx context.Context, additionalContext string) context.Context {
	return context.WithValue(ctx, ContextKeyAdditionalContext, additionalContext)
}

func ContextAdditionalContext(ctx context.Context, defaultContext string) string {
	return agent.ContextValue(ctx, ContextKeyAdditionalContext, defaultContext)
}

func WithContextKnowledgeBase(ctx context.Context, kb KnowledgeBase) context.Context {
	return context.WithValue(ctx, ContextKeyKnowledgeBase, kb)
}

func ContextKnowledgeBase(ctx context.Context) (KnowledgeBase, bool) {
	kb, ok := ctx.Value(ContextKeyKnowledgeBase).(KnowledgeBase)
	return kb, ok
}
