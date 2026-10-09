package defaults

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

const mathJaxFontDir = "static/fonts/mathjax-newcm-font/chtml"

// TestMathJaxFontsVendored guards against MathJax falling back to its CDN for
// fonts: every woff2 file the bundled MathJax names must be embedded, and the
// options must point MathJax's [fonts] path at the embedded copy.
func TestMathJaxFontsVendored(t *testing.T) {
	t.Parallel()

	bundle, err := fs.ReadFile(StaticFiles, "static/js/tex-mml-chtml.js")
	if err != nil {
		t.Fatalf("read MathJax bundle: %v", err)
	}
	refs := regexp.MustCompile(`mjx-ncm-[a-z0-9-]+\.woff2`).FindAllString(string(bundle), -1)
	if len(refs) == 0 {
		t.Fatal("expected the MathJax bundle to reference mjx-ncm-*.woff2 fonts")
	}
	for _, name := range refs {
		if _, err := fs.Stat(StaticFiles, mathJaxFontDir+"/woff2/"+name); err != nil {
			t.Errorf("font %s referenced by the MathJax bundle is not embedded: %v", name, err)
		}
	}

	dynamic, err := fs.ReadDir(StaticFiles, mathJaxFontDir+"/dynamic")
	if err != nil {
		t.Fatalf("read dynamic font data dir: %v", err)
	}
	if len(dynamic) == 0 {
		t.Fatal("expected dynamic font data files to be embedded")
	}

	options, err := fs.ReadFile(StaticFiles, "static/js/mathjax-options.js")
	if err != nil {
		t.Fatalf("read MathJax options: %v", err)
	}
	if !strings.Contains(string(options), "fonts: '/static/fonts'") {
		t.Fatalf("expected mathjax-options.js to set loader.paths.fonts to /static/fonts, got:\n%s", options)
	}
}
