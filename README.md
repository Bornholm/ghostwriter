# Ghostwriter

A little tool to generate sourced article about any subject with LLMs

## Usage

1. Create `.env` file

   ```shell
   GHOSTWRITER_CHAT_COMPLETION_API_KEY=<api-key>
   GHOSTWRITER_CHAT_COMPLETION_PROVIDER=<provider> # available: openai, openrouter, mistral
   GHOSTWRITER_CHAT_COMPLETION_BASE_URL=<api-base-url> # ex: https://openrouter.ai/api/v1
   GHOSTWRITER_CHAT_COMPLETION_MODEL=<model-name> # ex: google/gemini-2.5-flash
   ```

2. Generate your document:

   ```bash
   go run ./cmd/ghostwriter -subject "What are the latest news about 3I/ATLAS ?"
   ```
