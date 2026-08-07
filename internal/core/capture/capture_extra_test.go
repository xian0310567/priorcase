package capture

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 지시 1: Do() 가 노트를 쓴 뒤 색인 갱신에 실패하면 노트만 남고 색인이
// 낡는다. 색인 디렉토리를 읽기전용으로 만들어 실패를 유도하고, 에러
// 메시지가 "노트는 이미 저장됐다" 는 사실을 사용자에게 알려주는지,
// 그리고 노트 파일이 실제로 디스크에 남아 있는지를 확인한다.
func TestDoIndexWriteFailureLeavesNoteButReportsIt(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root 는 디렉토리 퍼미션을 무시하므로 이 테스트가 성립하지 않는다")
	}
	l, c := fixtureLayoutConfig(t)

	// 색인이 놓일 디렉토리를 미리 만들고 쓰기 권한을 뺏는다.
	// MkdirAll 은 디렉토리가 이미 있으면 성공하므로, index.Write 안의
	// os.MkdirAll 은 통과하고 그 다음 WriteFileAtomic 의 os.CreateTemp 가
	// 권한 부족으로 실패한다.
	metaDir := filepath.Dir(l.IndexPath())
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(metaDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(metaDir, 0o755) })

	req := Request{
		Domain: "alpha", Slug: "색인 실패 유도", Summary: "색인 쓰기가 실패해도 노트는 남아야 한다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	}
	_, err := Do(l, c, req)
	if err == nil {
		t.Fatal("색인 디렉토리를 읽기전용으로 만들었는데 Do() 가 성공했다")
	}
	if !strings.Contains(err.Error(), "노트는 썼으나") {
		t.Errorf("에러 메시지가 노트는 이미 저장됐다는 사실을 알려주지 않는다: %v", err)
	}

	path, perr := l.DecisionPath(req.Domain, req.Slug, req.Date)
	if perr != nil {
		t.Fatal(perr)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("노트 파일이 실제로는 남아 있어야 하는데 없다 (%s): %v", path, statErr)
	}
}

// 지시 2: 중복 검사가 os.Stat 하나뿐이다 — 같은 경로만 잡는다. 감사에서
// 나온 결함 4("slug 가 한 글자만 달라도 중복 노트가 생긴다")를 이
// 태스크에서 고치지는 않는다(범위 밖). 대신 현재 동작을 테스트로 못박아,
// 나중에 유사도 기반 검사를 넣을 때 이 테스트가 깨지면서 의도가
// 바뀌었음을 알 수 있게 한다.
func TestDoAllowsNearDuplicateSlugCurrently(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	r1 := Request{Domain: "alpha", Slug: "저장소 정책", Summary: "저장소 정책을 정한다",
		Date: "2026-08-07", Body: []byte("## 결정\n")}
	if _, err := Do(l, c, r1); err != nil {
		t.Fatal(err)
	}

	// slug 가 글자 하나(끝의 "1") 다를 뿐이지만 파일명이 달라지므로 지금은
	// 중복으로 취급되지 않고 그대로 통과한다.
	r2 := Request{Domain: "alpha", Slug: "저장소 정책1", Summary: "저장소 정책을 다시 정한다",
		Date: "2026-08-07", Body: []byte("## 결정\n")}
	if _, err := Do(l, c, r2); err != nil {
		t.Fatalf("비슷하지만 다른 slug 가 거부됐다 — 중복 검사가 os.Stat 하나뿐이라는 "+
			"현재 동작(범위 밖, 결함4)이 바뀐 것으로 보인다: %v", err)
	}
}

// 지시 3: 편승 검색은 쓰기 *전*에 한다 — 자기 자신이 결과에 끼지 않게.
// 방금 기록한 노트가 Result.Related 에 나타나지 않는지로 이 순서를
// 못박는다. 검색이 쓰기 *후*로 바뀌면 List() 가 방금 쓴 파일을 읽어
// 자기 자신을 최고 점수로 돌려주게 되어 이 테스트가 깨진다.
func TestDoRelatedExcludesJustWrittenNote(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장 엔진 자기 참조 확인", Summary: "저장 엔진을 다시 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Related {
		if h.Note.Path == res.Path {
			t.Errorf("편승 검색이 쓰기 후에 실행된 것으로 보인다 — 자기 자신이 "+
				"Related 에 포함됐다: %s", h.Note.Path)
		}
	}
}

// 지시 4: ensureDecisionTag 는 decision 태그를 맨 앞에 붙인다. 이미 있으면
// 그대로 둔다.
func TestEnsureDecisionTagPrependsWhenMissing(t *testing.T) {
	got := ensureDecisionTag([]string{"alpha", "저장엔진"})
	want := []string{"decision", "alpha", "저장엔진"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ensureDecisionTag() = %v, want %v", got, want)
	}
}

func TestEnsureDecisionTagLeavesExistingAlone(t *testing.T) {
	got := ensureDecisionTag([]string{"decision", "alpha", "저장엔진"})
	want := []string{"decision", "alpha", "저장엔진"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ensureDecisionTag() = %v, want %v (이미 있으면 그대로 둬야 한다)", got, want)
	}
}
