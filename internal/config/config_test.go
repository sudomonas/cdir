// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if c, w := Load(p); c != Default() || w != nil {
		t.Errorf("missing file: %+v %v", c, w)
	}
	os.WriteFile(p, []byte("# c\nshow_hidden = yes\nsort=alpha\nbogus=1\nshow_files=maybe\n"), 0o644)
	c, w := Load(p)
	if !c.ShowHidden || c.Sort != "alpha" || c.ShowFiles {
		t.Errorf("got %+v", c)
	}
	if len(w) != 2 {
		t.Errorf("want 2 warnings, got %v", w)
	}
}
