package capture

import (
	"os"
	"strings"
	"testing"
)

func TestDoCreatesNote(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "새 결정", Summary: "새 결정을 내렸다",
		Date: "2026-08-07", Tags: []string{"decision", "alpha"},
		Body: []byte("## 결정\n\n내용.\n"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !strings.HasSuffix(res.Path, "alpha-결정-새-결정-2026-08-07.md") {
		t.Errorf("경로가 규약과 다르다: %s", res.Path)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `summary: "새 결정을 내렸다"`) {
		t.Errorf("frontmatter 가 정본 형식이 아니다:\n%s", data)
	}
}

func TestDoRejectsSchemaViolation(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// summary 가 비면 거부
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "x", Date: "2026-08-07"}); err == nil {
		t.Fatal("summary 없는 요청을 통과시켰다")
	}
	// 알 수 없는 도메인은 거부
	if _, err := Do(l, c, Request{Domain: "없음", Slug: "x", Summary: "s", Date: "2026-08-07"}); err == nil {
		t.Fatal("알 수 없는 도메인을 통과시켰다")
	}
}

func TestDoRejectsDuplicate(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	r := Request{Domain: "alpha", Slug: "중복", Summary: "s", Date: "2026-08-07",
		Body: []byte("## 결정\n")}
	if _, err := Do(l, c, r); err != nil {
		t.Fatal(err)
	}
	if _, err := Do(l, c, r); err == nil {
		t.Fatal("같은 경로에 두 번 썼다")
	}
}

func TestDoReturnsRelated(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장 엔진 재검토", Summary: "저장 엔진을 다시 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 편승: 기록하면 관련 과거 결정이 딸려 나온다
	if len(res.Related) == 0 {
		t.Error("관련 결정이 반환되지 않았다 — 편승이 동작하지 않는다")
	}
}

// TestDoSupersedesBothSides 는 prior capture --supersedes 가 prior review 와 같은
// 일을 하는지 본다: 새 노트의 supersedes 가 "[[stem]]" 형식이고, 옛 노트가
// superseded 로 바뀌고 related 에 후속 문서가 들어간다. 예전에는 capture 가
// 날문자열을 그대로 넣기만 해서 옛 노트가 active 로 남았고, 이미 뒤집힌 결정이
// 회수에서 감점 없이 계속 1위로 올라왔다.
func TestDoSupersedesBothSides(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	oldStem := "alpha-결정-저장엔진-2026-08-01"

	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장엔진 재선정", Summary: "저장 엔진을 다시 고른다",
		Date: "2026-08-07", Supersedes: []string{oldStem}, Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	newNote, err := l.Read(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[[" + oldStem + "]]"
	if len(newNote.Meta.Supersedes) != 1 || newNote.Meta.Supersedes[0] != want {
		t.Errorf("새 노트 supersedes = %v, want [%q] (review 와 같은 형식이어야 한다)",
			newNote.Meta.Supersedes, want)
	}

	oldPath, err := l.ResolveStem(oldStem)
	if err != nil {
		t.Fatal(err)
	}
	old, err := l.Read(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if old.Meta.Status != "superseded" {
		t.Errorf("옛 노트 status = %q, want superseded — 회수 감점이 안 걸린다", old.Meta.Status)
	}
	backlink := "[[" + newNote.Stem + "]]"
	found := false
	for _, rel := range old.Meta.Related {
		if rel == backlink {
			found = true
		}
	}
	if !found {
		t.Errorf("옛 노트 related 에 후속 문서(%s)가 없다: %v", backlink, old.Meta.Related)
	}
}

// TestDoRejectsSupersedesTraversal 은 --supersedes 로 들어온 경로 순회 문자열이
// 거부되는지 본다. 예전에는 검증 없이 frontmatter 에 그대로 안착해서,
// ResolveStem 이 막아 놓은 경로 순회가 이 경로로 통째로 우회됐다.
func TestDoRejectsSupersedesTraversal(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	for _, bad := range []string{"../../CLAUDE", "../CLAUDE", "규약없음", "없는도메인-결정-x-2026-08-01"} {
		res, err := Do(l, c, Request{
			Domain: "alpha", Slug: "순회 시도 " + bad, Summary: "s",
			Date: "2026-08-07", Supersedes: []string{bad}, Body: []byte("## 결정\n"),
		})
		if err == nil {
			t.Errorf("--supersedes %q 를 통과시켰다 → %s", bad, res.Path)
			continue
		}
		if !strings.Contains(err.Error(), "supersedes") {
			t.Errorf("--supersedes %q 의 에러가 원인을 알려주지 않는다: %v", bad, err)
		}
	}
}

// TestDoValidatesBothNotesBeforeWritingEither 는 Review 와 같은 불변식이
// Do 에도 있는지 본다: 새 노트 검증이 실패하면 옛 노트는 손대지 않는다.
// 순서가 뒤집히면 "뒤집은 결정은 없는데 옛 결정만 superseded" 인 반쪽 상태가
// 디스크에 남는다.
func TestDoValidatesBothNotesBeforeWritingEither(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	oldStem := "alpha-결정-저장엔진-2026-08-01"
	oldPath, err := l.ResolveStem(oldStem)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	// summary 가 비면 새 노트가 스키마 검증에서 걸린다.
	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "요약 없는 결정", Summary: "",
		Date: "2026-08-07", Supersedes: []string{oldStem}, Body: []byte("## 결정\n"),
	}); err == nil {
		t.Fatal("summary 가 빈 요청을 통과시켰다")
	}

	after, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("새 노트 검증이 실패했는데 옛 노트가 이미 쓰였다(부분 실패):\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// --related 로 들어온 맨 stem 은 `[[ ]]` 로 감싸 저장한다.
//
// MCP 설명문이 "위키링크 **또는** stem" 이라 두 형식을 다 권한 탓에 실볼트에
// `[[ ]]` 없는 값이 남았다. 옵시디언은 그것을 링크로 읽지 않으므로 백링크 패널에
// 안 뜨는 죽은 문자열이 된다.
func TestDoNormalizesRelated(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "정규화", Summary: "s", Date: "2026-08-07",
		Related: []string{"beta-결정-배포전략-2026-08-03", "  [[common-결정-로케일함정-2026-08-04]]  "},
		Body:    []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := `related: ["[[beta-결정-배포전략-2026-08-03]]", "[[common-결정-로케일함정-2026-08-04]]"]`
	if !strings.Contains(string(data), want) {
		t.Errorf("related 가 정본 형식이 아니다.\nwant %s\n실제:\n%s", want, data)
	}
}

// ★ supersede.go 주석이 실측으로 기록한 사고: "../../CLAUDE" 가 frontmatter 에
// 그대로 안착했다. ResolveStem 이 막던 경로 순회를 --related 가 통째로 우회한다.
func TestDoRejectsPathEscapeInRelated(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	for _, bad := range []string{"../../CLAUDE", "[[../CLAUDE]]", "alpha/decisions/x", "[[]]"} {
		_, err := Do(l, c, Request{
			Domain: "alpha", Slug: "탈출" + bad, Summary: "s", Date: "2026-08-07",
			Related: []string{bad}, Body: []byte("## 결정\n"),
		})
		if err == nil {
			t.Errorf("related=%q 를 통과시켰다", bad)
		}
	}
}

// ★ 한 결정이 여럿을 뒤집을 수 있어야 한다.
//
// 실볼트 사례: 2026-08-13 `방향전환-개인도구-다중볼트` 가 전제 6개를 폐기 선언했는데
// `--supersedes` 가 한 칸뿐이라 1건만 엮였다. 나머지는 본문 산문으로 밀려났고,
// `유료층-순수E2E-복구불가`·`클라이언트비제공` 두 노트가 "superseded 인데 무엇이
// 뒤집었는지 아무 데도 없는" 상태로 남았다 — doctor 의 뒤집기 검사가 지금 그걸 문다.
func TestDoSupersedesMultipleTargets(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "방향전환", Summary: "전제를 한꺼번에 걷어낸다", Date: "2026-08-11",
		Supersedes: []string{"alpha-결정-스키마-2026-08-02", "beta-결정-배포전략-2026-08-03"},
		Body:       []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := `supersedes: ["[[alpha-결정-스키마-2026-08-02]]", "[[beta-결정-배포전략-2026-08-03]]"]`
	if !strings.Contains(string(data), want) {
		t.Errorf("supersedes 가 둘 다 안 적혔다.\nwant %s\n실제:\n%s", want, data)
	}
	// 양쪽 다 뒤집힌 것으로 기록돼야 한다 — 한쪽만 되면 회수 감점이 반만 걸린다.
	for _, stem := range []string{"alpha-결정-스키마-2026-08-02", "beta-결정-배포전략-2026-08-03"} {
		p, err := l.ResolveStem(stem)
		if err != nil {
			t.Fatal(err)
		}
		old, err := l.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		if old.Meta.Status != "superseded" {
			t.Errorf("%s status = %q, want superseded", stem, old.Meta.Status)
		}
		if !strings.Contains(strings.Join(old.Meta.Related, " "), "방향전환") {
			t.Errorf("%s related 에 후속이 없다: %v", stem, old.Meta.Related)
		}
	}
}
