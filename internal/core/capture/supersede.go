package capture

import (
	"fmt"

	"github.com/xian0310567/casebook/internal/core/store"
)

// supersede 는 "이 결정이 저 결정을 뒤집는다" 를 양방향으로 엮는다.
// cb capture --supersedes 와 cb review --supersedes 가 이 함수 하나를 쓴다.
//
// 예전에는 capture 가 받은 문자열을 검증도 감싸기도 없이 frontmatter 에 그대로
// 넣었고 review 만 제대로 처리했다. 실측된 결과 셋:
//
//  1. supersedes 값이 노트마다 두 형식(날문자열 / "[[stem]]")으로 갈렸다 —
//     스펙 §4.1 이 없애려던 "같은 일을 하는 코드 두 벌" 이 데이터에 생겼다.
//  2. capture 로 뒤집으면 옛 노트가 active 로 남아 회수 감점
//     (search.penaltySuperseded)이 안 걸렸다 — 이미 뒤집힌 결정이 만점으로
//     계속 올라온다.
//  3. "../../CLAUDE" 가 frontmatter 에 그대로 안착했다 — ResolveStem 이
//     심층방어까지 좁혀 놓은 경로 순회 검증이 이 경로에서 통째로 우회됐다.
//
// 디스크에는 쓰지 않는다. 호출부가 새 노트까지 검증한 뒤에 쓰기를 시작해야
// 하기 때문이다 — 옛 노트를 먼저 쓰고 새 노트 검증에서 실패하면 옛 노트만
// superseded 로 남아 양방향 연결이 반쪽짜리 상태로 디스크에 고정된다.
//
// 준다: 새 노트의 supersedes 에 넣을 위키링크와, status·related 를 갱신한 옛 노트.
func supersede(l *store.Layout, target, newStem string) (string, store.Note, error) {
	oldPath, err := l.ResolveStem(target)
	if err != nil {
		return "", store.Note{}, fmt.Errorf("supersedes 대상이 잘못됐다: %w", err)
	}
	// ResolveStem 이 NFC 로 접은 경로에서 stem 을 되읽는다 — 링크 문자열도
	// 디스크의 파일명과 같은 정규화 형태여야 위키링크가 걸린다.
	old, err := l.Read(oldPath)
	if err != nil {
		return "", store.Note{}, fmt.Errorf("대상 없음: %s (%w)", target, err)
	}
	if old.Stem == newStem {
		return "", store.Note{}, fmt.Errorf("자기 자신을 뒤집을 수 없다: %s", newStem)
	}
	old.Meta.Status = "superseded"
	old.Meta.Related = appendUnique(old.Meta.Related, "[["+newStem+"]]")
	return "[[" + old.Stem + "]]", old, nil
}
