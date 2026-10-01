package genutil

import "testing"

func TestCommentBlock(t *testing.T) {
	if got := CommentBlock("  \n"); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got, want := CommentBlock("\n a\n\n b \n"), "// a\n// \n// b\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSPDXID(t *testing.T) {
	if got := SPDXID("", "x"); got != "" {
		t.Errorf("empty id = %q", got)
	}
	if got := SPDXID("LGPL-2.1", "x"); got != "LGPL-2.1" {
		t.Errorf("got %q", got)
	}
	if got := SPDXID("LGPL-2.1", "a\n  SPDX-License-Identifier: LGPL-2.1\n"); got != "" {
		t.Errorf("already present = %q", got)
	}
}
