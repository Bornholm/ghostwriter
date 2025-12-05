package main

import (
	"github.com/bornholm/ghostwriter/internal/command"
	"github.com/bornholm/ghostwriter/internal/command/write"

	_ "github.com/bornholm/genai/llm/provider/all"
)

var (
	version string = "dev"
)

func main() {
	command.Main(
		"ghostwriter", version, "write/edit articles with LLMs",
		write.Root(),
	)
}
