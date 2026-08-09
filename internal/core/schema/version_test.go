package schema_test

import (
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
)

func meta(schemaN int, status, outcome string) store.Meta {
	return store.Meta{
		Type: "decision", Date: "2026-08-09", Domain: []string{"alpha"},
		Summary: "x", Status: status, Outcome: outcome, Schema: schemaN,
	}
}

// ★★ 팀이 볼트를 공유하면 버전이 갈린다. 한 명이 먼저 올리면 나머지가 그 사람의
// 노트를 만나는데, 옛 바이너리가 새 값을 "허용값 밖" 으로 거부하면 **남의 결정을
// 지우는 것과 같다.**
func TestFutureNoteEnumValuesAreAccepted(t *testing.T) {
	l := store.NewLayout(testutil.VaultConfig(t))
	// 아직 우리가 모르는 status·outcome — 미래 판이 쓴 것이다.
	m := meta(schema.Current+1, "archived", "mixed")
	if err := schema.Validate(l.DecisionMarker(), "alpha-결정-x-2026-08-09", m); err != nil {
		t.Errorf("더 새 판의 노트를 거부했다: %v", err)
	}
}

// 같은 판이면 여전히 엄격하다 — 오탈자를 통과시키면 안 된다.
func TestCurrentSchemaStillStrict(t *testing.T) {
	l := store.NewLayout(testutil.VaultConfig(t))
	for _, m := range []store.Meta{
		meta(0, "archived", "pending"), // schema 미기재 = 1
		meta(schema.Current, "active", "mixed"),
	} {
		if err := schema.Validate(l.DecisionMarker(), "alpha-결정-x-2026-08-09", m); err == nil {
			t.Errorf("현재 판인데 모르는 값을 통과시켰다: %+v", m)
		}
	}
}

// 구조는 판이 올라가도 검사한다 — summary 가 비면 회수가 죽는다.
func TestFutureNoteStillNeedsStructure(t *testing.T) {
	l := store.NewLayout(testutil.VaultConfig(t))
	m := meta(schema.Current+1, "archived", "mixed")
	m.Summary = ""
	if err := schema.Validate(l.DecisionMarker(), "alpha-결정-x-2026-08-09", m); err == nil {
		t.Error("summary 가 비었는데 통과했다 — 회수 시 주입할 것이 없다")
	}
}

// ★ 읽는 것은 안전하지만 **쓰는 것**은 다르다. 우리가 모르는 규칙으로 쓰인 노트를
// 우리 규칙으로 되쓰면 조용히 망가뜨린다.
func TestCannotModifyFutureNote(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)

	// 미래 판 노트를 심는다.
	res, err := capture.Do(l, c, capture.Request{
		Domain: "alpha", Slug: "미래결정", Summary: "미래 판이 쓴 것",
		Date: "2026-08-09", Body: []byte("## 결정\n\nx\n")})
	if err != nil {
		t.Fatal(err)
	}
	n, err := l.Read(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	n.Meta.Schema = schema.Current + 1
	n.Meta.Status = "archived"
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}

	_, err = capture.Review(l, capture.ReviewRequest{
		Stem: n.Stem, Outcome: "good"})
	if err == nil {
		t.Fatal("더 새 판의 노트를 고쳤다 — 조용히 망가뜨릴 수 있다")
	}
	if !strings.Contains(err.Error(), "cb 를 올려라") {
		t.Errorf("왜 멈추는지 안 알려준다: %v", err)
	}

	// 노트는 그대로여야 한다.
	after, err := l.Read(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Meta.Status != "archived" || after.Meta.Schema != schema.Current+1 {
		t.Errorf("멈춘다면서 고쳤다: status=%q schema=%d", after.Meta.Status, after.Meta.Schema)
	}
}

// 판 1 은 방출하지 않는다 — 기존 노트가 재기록될 때 바이트가 안 바뀐다.
func TestSchemaOneIsNotEmitted(t *testing.T) {
	for _, n := range []int{0, 1} {
		out := string(store.EmitFrontmatter(meta(n, "active", "pending")))
		if strings.Contains(out, "schema:") {
			t.Errorf("판 %d 인데 schema 를 적었다 — 기존 노트의 바이트가 바뀐다:\n%s", n, out)
		}
	}
	out := string(store.EmitFrontmatter(meta(2, "active", "pending")))
	if !strings.Contains(out, "schema: 2") {
		t.Errorf("판 2 를 안 적었다:\n%s", out)
	}
}
