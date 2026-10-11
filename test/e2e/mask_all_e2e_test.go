package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/automationd/screenpear/services"
)

// TestMaskSubstituteAll masks every fixture with substitution on and no
// rules: the "real screenshot" path, where nothing in the dictionary
// matches. Outputs land in test/results/<ts>-TestMaskSubstituteAll/ for
// eyeballing style, size and placement against test/testdata/input.
func TestMaskSubstituteAll(t *testing.T) {
	svc := newOCR(t)
	dir := resultDir(t)
	for _, path := range inputFixtures(t) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			a, err := svc.Analyze(path)
			if err != nil {
				t.Fatal(err)
			}
			res, err := svc.Mask(services.MaskRequest{
				Path: path, Output: filepath.Join(dir, services.DefaultOutputPath(filepath.Base(path))),
				Lines: a.Lines, SubstituteAll: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			writeJSON(t, filepath.Join(dir, name+"-mask.json"), res)
			if len(res.Replaced) < len(a.Lines)/2 {
				t.Errorf("%s: only %d of %d lines replaced", name, len(res.Replaced), len(a.Lines))
			}
			t.Logf("%s: %d/%d lines replaced in %d ms", name, len(res.Replaced), len(a.Lines), res.ElapsedMS)
		})
	}
}
