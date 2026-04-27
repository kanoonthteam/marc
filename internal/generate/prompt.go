package generate

import (
	_ "embed"
	"os"
)

//go:embed prompts/question_gen.md
var embeddedQuestionGenPrompt string

// overridePath is the filesystem path operators can use to override the
// embedded question_gen prompt without restarting the server. If the file
// exists and is readable at call time its content is returned; otherwise the
// embedded default is returned.
const overridePath = "/etc/marc/prompts/question_gen.md"

// questionGenPrompt returns the prompt text to use for question generation.
//
// Override logic: if /etc/marc/prompts/question_gen.md exists and is readable,
// its content is returned. The check happens on every call (cheap: one
// os.Stat + small file read) so that operators can update the prompt without
// restarting marc-server.
func questionGenPrompt() string {
	if _, err := os.Stat(overridePath); err == nil {
		data, err := os.ReadFile(overridePath)
		if err == nil {
			return string(data)
		}
		// If ReadFile fails, fall through to the embedded default.
	}
	return embeddedQuestionGenPrompt
}
