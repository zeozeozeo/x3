package commands

import (
	"testing"

	"github.com/zeozeozeo/x3/codeinterp"
)

func TestRunnableCodeBlocks(t *testing.T) {
	content := "before\n```py\nprint('hi')\n```\n```js\nconsole.log('yes')\n```\n```ruby\nnope()\n```\n```lua\nprint('ok')\n```"
	blocks := runnableCodeBlocks(content)
	if len(blocks) != 3 || blocks[0].Language != "python" || blocks[1].Language != "javascript" || blocks[2].Language != "lua" {
		t.Fatalf("unexpected blocks: %#v", blocks)
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
