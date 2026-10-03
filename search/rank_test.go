package search

import "testing"

func TestBM25RanksShorterMatchAheadOfUnrelated(t *testing.T) {
	docs := []Document{
		Analyze("dogs-long", "dogs", "dogs and cats share a yard"),
		Analyze("dogs-short", "dogs", "dogs"),
		Analyze("physics", "physics", "quantum physics"),
	}
	ranked := BM25(docs, Tokens("dogs"))
	if len(ranked) != 3 {
		t.Fatalf("len = %d", len(ranked))
	}
	if ranked[0].ID != "dogs-short" {
		t.Fatalf("first = %s", ranked[0].ID)
	}
	if ranked[2].ID != "physics" || ranked[2].Score != 0 {
		t.Fatalf("last = %+v", ranked[2])
	}
}

func TestFusePrefersItemHighInBothLists(t *testing.T) {
	fused := Fuse([][]string{{"a", "b", "c"}, {"b", "c", "a"}}, 60)
	if len(fused) == 0 || fused[0] != "b" {
		t.Fatalf("fused = %v", fused)
	}
}

func TestPageUsesCursorAndCapsLimit(t *testing.T) {
	ids, next := Page([]string{"a", "b", "c", "d"}, "", 2)
	if len(ids) != 2 || ids[0] != "a" || next != "b" {
		t.Fatalf("ids=%v next=%s", ids, next)
	}
	ids, next = Page([]string{"a", "b", "c", "d"}, "b", 2)
	if len(ids) != 2 || ids[0] != "c" || next != "" {
		t.Fatalf("ids=%v next=%s", ids, next)
	}
	ids, next = Page([]string{"a"}, "missing", 10)
	if ids != nil || next != "" {
		t.Fatalf("ids=%v next=%s", ids, next)
	}
}
