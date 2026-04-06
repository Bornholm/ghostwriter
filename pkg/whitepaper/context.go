package whitepaper

import (
	"context"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/ghostwriter/pkg/article"
)

const (
	ctxKeySubject          agent.ContextKey = "whitepaper_subject"
	ctxKeyTargetWordCount  agent.ContextKey = "whitepaper_target_word_count"
	ctxKeyResearchDepth    agent.ContextKey = "whitepaper_research_depth"
	ctxKeyStyleGuidelines  agent.ContextKey = "whitepaper_style_guidelines"
	ctxKeyAdditionalCtx    agent.ContextKey = "whitepaper_additional_context"
	ctxKeyKnowledgeBase    agent.ContextKey = "whitepaper_knowledge_base"
	ctxKeySearcher         agent.ContextKey = "whitepaper_knowledge_searcher"
	ctxKeyPlan             agent.ContextKey = "whitepaper_plan"
	ctxKeyChapter          agent.ContextKey = "whitepaper_chapter"
	ctxKeyPreviousChapter  agent.ContextKey = "whitepaper_previous_chapter"
	ctxKeyAllChapters      agent.ContextKey = "whitepaper_all_chapters"
	ctxKeyAnnotations      agent.ContextKey = "whitepaper_annotations"
)

func withCtxSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, ctxKeySubject, subject)
}

func ctxSubject(ctx context.Context, def string) string {
	return agent.ContextValue(ctx, ctxKeySubject, def)
}

func withCtxTargetWordCount(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, ctxKeyTargetWordCount, n)
}

func ctxTargetWordCount(ctx context.Context, def int) int {
	return agent.ContextValue(ctx, ctxKeyTargetWordCount, def)
}

func withCtxResearchDepth(ctx context.Context, d article.ResearchDepth) context.Context {
	return context.WithValue(ctx, ctxKeyResearchDepth, d)
}

func ctxResearchDepth(ctx context.Context) article.ResearchDepth {
	return agent.ContextValue(ctx, ctxKeyResearchDepth, article.ResearchDeep)
}

func withCtxStyleGuidelines(ctx context.Context, s string) context.Context {
	return context.WithValue(ctx, ctxKeyStyleGuidelines, s)
}

func ctxStyleGuidelines(ctx context.Context) string {
	return agent.ContextValue(ctx, ctxKeyStyleGuidelines, "")
}

func withCtxAdditionalContext(ctx context.Context, s string) context.Context {
	return context.WithValue(ctx, ctxKeyAdditionalCtx, s)
}

func ctxAdditionalContext(ctx context.Context) string {
	return agent.ContextValue(ctx, ctxKeyAdditionalCtx, "")
}

func withCtxKnowledgeBase(ctx context.Context, kb article.KnowledgeBase) context.Context {
	return context.WithValue(ctx, ctxKeyKnowledgeBase, kb)
}

func ctxKnowledgeBase(ctx context.Context) (article.KnowledgeBase, bool) {
	kb, ok := ctx.Value(ctxKeyKnowledgeBase).(article.KnowledgeBase)
	return kb, ok
}

func withCtxSearcher(ctx context.Context, ks KnowledgeSearcher) context.Context {
	return context.WithValue(ctx, ctxKeySearcher, ks)
}

func ctxSearcher(ctx context.Context) (KnowledgeSearcher, bool) {
	ks, ok := ctx.Value(ctxKeySearcher).(KnowledgeSearcher)
	return ks, ok
}

func withCtxPlan(ctx context.Context, plan WhitePaperPlan) context.Context {
	return context.WithValue(ctx, ctxKeyPlan, plan)
}

func ctxPlan(ctx context.Context) (WhitePaperPlan, bool) {
	plan, ok := ctx.Value(ctxKeyPlan).(WhitePaperPlan)
	return plan, ok
}

func withCtxChapter(ctx context.Context, ch *Chapter) context.Context {
	return context.WithValue(ctx, ctxKeyChapter, ch)
}

func ctxChapter(ctx context.Context) (*Chapter, bool) {
	ch, ok := ctx.Value(ctxKeyChapter).(*Chapter)
	return ch, ok
}

func withCtxPreviousChapter(ctx context.Context, cc *ChapterContent) context.Context {
	return context.WithValue(ctx, ctxKeyPreviousChapter, cc)
}

func ctxPreviousChapter(ctx context.Context) *ChapterContent {
	cc, _ := ctx.Value(ctxKeyPreviousChapter).(*ChapterContent)
	return cc
}

func withCtxAllChapters(ctx context.Context, ccs []ChapterContent) context.Context {
	return context.WithValue(ctx, ctxKeyAllChapters, ccs)
}

func ctxAllChapters(ctx context.Context) ([]ChapterContent, bool) {
	ccs, ok := ctx.Value(ctxKeyAllChapters).([]ChapterContent)
	return ccs, ok
}

func withCtxAnnotations(ctx context.Context, annotations []string) context.Context {
	return context.WithValue(ctx, ctxKeyAnnotations, annotations)
}

func ctxAnnotations(ctx context.Context) []string {
	annotations, _ := ctx.Value(ctxKeyAnnotations).([]string)
	return annotations
}
