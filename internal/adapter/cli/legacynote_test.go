package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// legacyNoteBody 는 다른 도구가 남긴 구 스키마 frontmatter 다
// (title/project/created/superseded-by). `type: decision` 표식이 없어서 우리 노트가
// 아닌 것으로 걸린다 — 이런 노트가 섞여도 회수·기록이 **조용히** 줄어들지 않는 것이
// 이 픽스처를 쓰는 테스트들의 요지다.
//
// 이 픽스처는 원래 `index_test.go` 에 있었다. 색인을 없애면서(2026-08-24) 그 파일을
// 지웠는데 capture·recall·review 테스트 넷이 이 헬퍼를 쓰고 있었다 — 그래서 여기로
// 옮겼다. 지운 파일에 남의 픽스처가 살고 있는 것을 컴파일러가 잡아 줬다.
const legacyNoteBody = `---
title: 구 스키마로 쓰인 저장 엔진 결정
project: alpha
created: 2026-01-02
superseded-by: ""
---

## 결정

옛 도구가 남긴 형식이다.
`

// plantLegacyNote 는 픽스처 볼트의 alpha 결정 폴더에 구 스키마 노트를 하나 심고
// 볼트 기준 상대 경로를 준다.
func plantLegacyNote(t *testing.T, vault string) string {
	t.Helper()
	rel := filepath.Join("alpha", "decisions", "alpha-결정-저장엔진구형-2026-01-02.md")
	if err := os.WriteFile(filepath.Join(vault, rel), []byte(legacyNoteBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}
