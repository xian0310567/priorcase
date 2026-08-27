package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TypeRule 은 규칙 노트의 frontmatter 표식이다.
const TypeRule = "rule"

// rulesDirRel 은 규칙 노트가 사는 자리다. 볼트 상대 경로다.
//
// **설정 키로 빼지 않는다.** 동의어 표(search/synonym.go)와 같은 이유다 —
// 설정은 머신 사이를 건너오지 않는다. 2026-08-24 실측: 집에서 만든 novels·tutela
// 도메인이 회사 머신의 `config.toml` 에 없어 결정 23건이 통째로 안 보였다.
// 규칙은 **전 프로젝트에 걸리는 것이 존재 이유**라, 그것이 머신 하나에서 조용히
// 꺼지면 이 기능은 없는 것과 같다. 볼트는 git 으로 오간다.
const rulesDirRel = "_meta/rules"

// ── 규칙 노트란 무엇인가 ──────────────────────────────────────────────
//
// **결정에서 뽑아낸, 도메인이 없는 판단 기준이다.**
//
// # 고치려는 고장
//
// 2026-08-27 실측: 볼트 결정 414건의 summary 중 규칙·기준 어휘를 담은 것이
// 99건(24%)이고 나머지 76%는 사건 서술이다("GP-1561 실동작 검증 완료, 주소는
// wcms", "admin-api 태스크 역할 신설"). 사건은 정의상 그 프로젝트에서만 참이다.
//
// 전이되는 것은 "downside 가 0 이면 검증보다 먼저 넣는다" 같은 규칙인데, 그 규칙이
// `editup-결정-gp1561-…` 이라는 **사건 이름 안에 갇혀 있다.** 회수 단위가 노트라서
// 규칙 하나를 꺼낼 수가 없다 — 꺼내려면 그 사건을 아는 낱말로 물어야 하고, 다른
// 프로젝트에서 일하는 사람은 그 낱말을 모른다.
//
// 실제로 사업주는 이미 규칙을 쓰고 있었다. `common-결정-포트폴리오규칙승격` 의
// R1~R4, `tutela-결정-eu-compliance-widget-entry` 의 "기준 7번" 이 그것이다.
// 그 결정문은 **왜 그것이 필요한지도 직접 적었다** — "2026-08-23 novels 결정을 쓸
// 때 R1 이 회수되지 않았고 novels 는 'AI로 웹소설 여러 개 찍어내기'가 출발점이라
// R1 이 그 가설의 직접 반증인데 한 줄도 안 들어가 사후 보강해야 했다."
// 규칙이 mesh 폴더에 갇혀 있어서 novels 에서 안 나온 것이다.
//
// # 왜 도메인을 주지 않는가
//
// 도메인은 회수 경로다(common-결정-도메인배열이회수경로다-폴더와분리). 규칙에
// 도메인을 주면 그 순간 "어느 프로젝트의 규칙" 이 되고, `weightCwdDomain` 이
// 자기 폴더에서만 가점을 준다. **규칙은 전 프로젝트에 걸려야 하므로 도메인이
// 없는 것이 맞다** — 가점도 못 받지만 자기 슬롯이 따로 있어서 밀려나지도 않는다
// (search.Options.RuleLimit).
//
// # 왜 결정 폴더가 아니라 _meta 인가
//
// ① 결정 노트의 파일명 규약(`{domain}-결정-{slug}-{date}`)은 도메인을 요구한다.
// ② `List()` 가 도메인 폴더만 훑으므로 규칙이 결정 목록에 섞이면 그 슬롯을
// 빼앗는다 — 참고 문서에 자리를 따로 준 것과 같은 이유다.
// ③ `_meta` 는 이미 사람이 손으로 관리하는 구역이다(네이밍 규약·동의어 표).
// 규칙은 증류물이라 자동 생성이 아니라 큐레이션이 맞다.

// RulesDir 은 규칙 노트 폴더의 절대 경로다 (doctor 가 사람에게 알려 줄 때 쓴다).
func (l *Layout) RulesDir() string {
	return filepath.Join(l.vault, filepath.FromSlash(rulesDirRel))
}

// RulesDirRel 은 같은 자리의 볼트 상대 경로다.
func (l *Layout) RulesDirRel() string { return rulesDirRel }

// ListRules 는 규칙 노트를 준다.
//
// **폴더가 없는 것은 에러가 아니다.** 대다수 볼트에 이 폴더가 없고 없는 것이
// 정상이다 — 그때 점수 계산은 규칙이 없던 때와 한 바이트도 다르지 않다.
// 켜는 것은 파일을 만드는 행위 하나다(동의어 표와 같은 설계).
//
// 하위 폴더도 훑는다. 규칙이 늘면 사람이 묶고 싶어질 자리이고, 묶었다고 회수에서
// 빠지면 그건 놀라움이다.
func (l *Layout) ListRules() ([]Note, []SkippedNote, error) {
	root := l.RulesDir()
	var notes []Note
	var skipped []SkippedNote

	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir // 폴더가 아직 없다 — 정상이다
			}
			return nil
		}
		if e.IsDir() {
			return nil
		}
		if !strings.HasSuffix(NFC(e.Name()), ".md") {
			return nil
		}
		n, rerr := l.readRule(p)
		if rerr != nil {
			skipped = append(skipped, SkippedNote{Path: p, Reason: rerr})
			return nil
		}
		if n.Stem == "" {
			return nil // 참여하지 않는 문서 (type 이 rule 이 아니거나 summary 가 없다)
		}
		notes = append(notes, n)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("규칙 폴더를 훑을 수 없다 (%s): %w", root, err)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return notes, skipped, nil
}

// readRule 은 규칙 노트 하나를 읽는다.
//
// **`type: rule` 과 summary 가 표식이다.** 그 폴더에 README 나 초안이 있을 수
// 있고, 그건 고장이 아니라 정상이다 — 빈 Note 로 준다. 회수는 head(요약·제목·태그)에
// 아무것도 안 걸리면 그 문서를 버리므로 summary 없이는 실제로 아무 일도 안 난다.
//
// **frontmatter 가 깨진 것은 여전히 실패다.** 조용히 빠지면 "그런 규칙이 없다" 와
// 구별되지 않는다 — 규칙은 몇 건뿐이라 한 건이 빠지는 손해가 결정보다 크다.
func (l *Layout) readRule(path string) (Note, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	m, body, err := ParseFrontmatter(b)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			return Note{}, nil // 평범한 마크다운 — 참여하지 않는다
		}
		return Note{}, err
	}
	if m.Type != TypeRule {
		return Note{}, nil
	}
	if strings.TrimSpace(m.Summary) == "" {
		return Note{}, nil
	}
	// **도메인을 비운다.** 파일에 적혀 있어도 무시한다 — 규칙이 도메인을 가지면
	// 그 폴더에서만 가점을 받고, 그건 규칙을 만든 이유와 반대다(위 § 참고).
	m.Domain = nil
	return Note{
		Path: path,
		Stem: strings.TrimSuffix(NFC(filepath.Base(path)), ".md"),
		Meta: m,
		Body: body,
	}, nil
}

// IsRule 은 이 노트가 규칙인지 본다.
//
// 회수 결과에서 **규칙·결정·참고를 반드시 갈라 보여 줘야 한다.** 규칙은 도메인이
// 없어서 "어디서 온 것인가" 가 경로에만 있고, 결정처럼 그리면 읽는 쪽이 그것을
// 특정 프로젝트의 결정으로 오해한다.
func (n Note) IsRule() bool { return n.Meta.Type == TypeRule }
