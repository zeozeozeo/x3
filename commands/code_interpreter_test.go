package commands

import (
	"testing"

	"github.com/zeozeozeo/x3/codeinterp"
)

func TestRunnableCodeBlocks(t *testing.T) {
	content := "before\n```py\nprint('hi')\n```\n```js\nconsole.log('yes')\n```\n```ruby\nputs 'yes'\n```\n```lua\nprint('ok')\n```\n```brainfuck\nnope\n```"
	blocks := runnableCodeBlocks(content)
	if len(blocks) != 4 || blocks[0].Language != "python" || blocks[1].Language != "javascript" || blocks[2].Language != "ruby" || blocks[3].Language != "lua" {
		t.Fatalf("unexpected blocks: %#v", blocks)
	}
}

func TestLanguageDisplayName(t *testing.T) {
	tests := map[string]string{"javascript": "JavaScript", "typescript": "TypeScript", "php": "PHP", "shell": "Bash", "ruby": "Ruby"}
	for language, want := range tests {
		if got := languageDisplayName(language); got != want {
			t.Errorf("languageDisplayName(%q) = %q, want %q", language, got, want)
		}
	}
}

func TestConsumeGeneratedArtifacts(t *testing.T) {
	response, files := consumeGeneratedArtifacts("data: <file>results.csv</file> missing <file>no.png</file>", []codeinterp.Artifact{{Name: "results.csv", Data: []byte("x,y\n1,2\n")}})
	if response != "data:  missing" {
		t.Fatalf("response = %q", response)
	}
	if len(files) != 1 || files[0].Name != "results.csv" {
		t.Fatalf("files = %#v", files)
	}
}

func TestFormatCodeRunResultEscapesFences(t *testing.T) {
	got := formatCodeRunResult(codeinterp.Result{ExitCode: 1, Stderr: "bad ``` fence"})
	if got == "" || got == "bad ``` fence" {
		t.Fatalf("unexpected result %q", got)
	}
}
