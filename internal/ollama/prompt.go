package ollama

import (
	_ "embed"
	"os"
)

//go:embed prompts/denoise.md
var embeddedDenoisePrompt string

// overridePath is the filesystem path operators can use to override the
// embedded denoise prompt without restarting the server. If the file exists
// and is readable at call time its content is returned; otherwise the
// embedded default is returned.
const overridePath = "/etc/marc/prompts/denoise.md"

// denoisePrompt returns the prompt text to use for denoising.
//
// Override logic: if /etc/marc/prompts/denoise.md exists and is readable, its
// content is returned. The check happens on every call (cheap: one os.Stat +
// small file read) so that operators can update the prompt without restarting
// marc-server.
func denoisePrompt() string {
	if _, err := os.Stat(overridePath); err == nil {
		// Override file exists and stat succeeded — attempt to read it.
		data, err := os.ReadFile(overridePath)
		if err == nil {
			return string(data)
		}
		// If ReadFile fails, fall through to the embedded default.
	}
	return embeddedDenoisePrompt
}
