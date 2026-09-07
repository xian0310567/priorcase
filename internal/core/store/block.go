package store

import (
	"fmt"
	"strings"
)

// ★★★ **남의 파일에는 우리 구간만 남긴다.**
//
// `AGENTS.md`·`CLAUDE.md`·`.cursorrules` 는 **사용자가 쓴 문서**다. 거기에 회수
// 안내를 넣어야 하는데, 지금 있는 길(`prior brief --out`)은 파일을 통째로
// 덮어쓴다 — 사람이 그 플래그를 자기 AGENTS.md 로 겨누는 순간 그 문서가 사라진다.
// 되돌릴 수 없고, 그 사람은 우리가 자기 지침을 지웠다는 것을 나중에야 안다.
//
// 그래서 **표시로 감싼 구간만** 관리한다:
//
//	<!-- priorcase:start -->
//	…우리가 쓰는 것…
//	<!-- priorcase:end -->
//
// # 두 번 돌려도 같아야 한다
//
// 배선 명령은 사람이 여러 번 돌린다 — 설정을 바꾸고 다시, 새 머신에서 또. 그때마다
// 블록이 하나씩 늘면 그 문서는 우리 안내로 도배된다. 있으면 **갈아 끼우고**,
// 없으면 끝에 붙인다.
//
// # 표시가 반쪽이면 손대지 않는다
//
// 사람이 손으로 고치다 한쪽을 지울 수 있다. 그때 남은 한쪽을 기준으로 자르면
// 그 사람의 글을 먹는다. 반쪽이면 **에러로 알리고 아무것도 안 한다** — 조용히
// 추측해서 자르는 것보다 낫다.

const (
	blockStart = "<!-- priorcase:start -->"
	blockEnd   = "<!-- priorcase:end -->"
)

// SpliceBlock 은 기존 글에 관리 구간을 끼워 넣거나 갈아 끼운다.
//
// body 는 표시 사이에 들어갈 내용이다 (표시는 이 함수가 붙인다).
// 바뀐 것이 없으면 changed=false 다 — 호출부가 "이미 최신이다" 를 말할 수 있어야
// 매번 "썼다" 라고 보고해서 사람이 무엇이 바뀐 줄 알게 되는 일을 막는다.
func SpliceBlock(existing, body string) (out string, changed bool, err error) {
	block := blockStart + "\n" + strings.TrimRight(body, "\n") + "\n" + blockEnd

	i := strings.Index(existing, blockStart)
	j := strings.Index(existing, blockEnd)
	switch {
	case i < 0 && j < 0:
		// 없다 — 끝에 붙인다. 사람의 글과 한 줄 띄운다.
		if strings.TrimSpace(existing) == "" {
			return block + "\n", true, nil
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n", true, nil
	case i < 0 || j < 0:
		return "", false, fmt.Errorf(
			"priorcase 표시가 반쪽만 있다 — 손으로 고치다 한쪽이 지워진 것 같다. "+
				"%q 와 %q 를 짝맞춰 두거나 둘 다 지우고 다시 돌려라", blockStart, blockEnd)
	case j < i:
		return "", false, fmt.Errorf(
			"priorcase 표시의 순서가 뒤집혀 있다 (%q 가 %q 보다 앞이다)", blockEnd, blockStart)
	}

	out = existing[:i] + block + existing[j+len(blockEnd):]
	return out, out != existing, nil
}

// HasBlock 은 그 글에 우리 구간이 있는지 본다.
func HasBlock(s string) bool {
	return strings.Contains(s, blockStart) && strings.Contains(s, blockEnd)
}
