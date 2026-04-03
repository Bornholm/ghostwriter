package whitepaper

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/Bornholm/amatl/pkg/html/layout"
	"github.com/Bornholm/amatl/pkg/markdown/directive/attrs"
	"github.com/Bornholm/amatl/pkg/markdown/directive/toc"
	"github.com/Bornholm/amatl/pkg/pipeline"
	"github.com/Bornholm/amatl/pkg/resolver"
	arender "github.com/Bornholm/amatl/pkg/command/cli/render"
	"github.com/pkg/errors"
)

// RenderFormat is the output format for rendering.
type RenderFormat string

const (
	RenderFormatHTML RenderFormat = "html"
	RenderFormatPDF  RenderFormat = "pdf"
)

// RenderOptions configures the amatl rendering pass.
type RenderOptions struct {
	Format       RenderFormat
	OutputPath   string
	ChromiumPath string
	NoSandbox    bool
}

// RenderWhitePaper reads index.md from the output directory and renders it via amatl.
func RenderWhitePaper(ctx context.Context, indexMDPath string, opts RenderOptions) error {
	source, err := os.ReadFile(indexMDPath)
	if err != nil {
		return errors.Wrap(err, "could not read index.md")
	}

	absIndex, err := filepath.Abs(indexMDPath)
	if err != nil {
		return errors.WithStack(err)
	}

	sourcePath := resolver.Path(absIndex)
	baseDir, err := sourcePath.Dir().Abs()
	if err != nil {
		return errors.WithStack(err)
	}

	pipelineCtx := resolver.WithWorkDir(ctx, baseDir)

	middlewares := []pipeline.Middleware{
		arender.MarkdownMiddleware(
			arender.WithSourcePath(sourcePath),
			arender.WithIgnoredDirectives(toc.Type, attrs.Type),
		),
		arender.TemplateMiddleware(),
		arender.HTMLMiddleware(
			arender.WithMarkdownTransformerOptions(
				arender.WithSourcePath(sourcePath),
			),
			arender.WithLayoutURL(layout.DefaultRawURL),
		),
	}

	if opts.Format == RenderFormatPDF {
		pdfOpts := []arender.PDFTransformerOptionFunc{}
		if opts.ChromiumPath != "" {
			pdfOpts = append(pdfOpts, arender.WithExecPath(opts.ChromiumPath))
		}
		if opts.NoSandbox {
			pdfOpts = append(pdfOpts, arender.WithNoSandbox(true))
		}
		middlewares = append(middlewares, arender.PDFMiddleware(pdfOpts...))
	}

	transformer := pipeline.Pipeline(middlewares...)
	payload := pipeline.NewPayload(source)

	if err := transformer.Transform(pipelineCtx, payload); err != nil {
		return errors.Wrap(err, "rendering failed")
	}

	out, err := os.Create(opts.OutputPath)
	if err != nil {
		return errors.Wrap(err, "could not create output file")
	}
	defer out.Close()

	if _, err := io.Copy(out, payload.Buffer()); err != nil {
		return errors.WithStack(err)
	}

	return nil
}
