package health

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// checkBodyLinks 는 **본문의 위키링크**가 실재하는 문서를 가리키는지 본다.
//
// # 이것은 앞선 결정을 뒤집는다
//
// [[priorcase-결정-링크무결성과-회수가중치-여섯건-계획과다른선택셋-2026-08-15]] 는
// 본문 검사를 **일부러 뺐다.** 근거는 실측이었다 — 위키링크 291개 중 대상 없는 것이
// 15개인데 진짜는 1개뿐, **오탐률 93%.** 나머지는 ```toml 펜스 안의 `[[domain]]`,
// `[[옛이름]]` 자리표시자, `[[벧전 5:7]]` 성경 인용이었다.
//
// 그 판단은 그때 옳았다. 지금 뒤집는 이유는 **숫자가 뒤집혔기 때문**이다.
//
// # 실측 (2026-08-27, 볼트 654문서)
//
//	본문 위키링크         3,644개
//	대상 없음                62개 (1.70%)
//	  자리표시자·문법          48개
//	  성경 인용                 3개
//	  모양 필터 통과           14개  ← 사람이 봐야 할 것
//	    그중 진짜 깨짐         13개  (정밀도 93%)
//
// **93% 오탐이 93% 적중으로 뒤집혔다.** 볼트가 12배로 커지는 동안 자리표시자는
// 거의 안 늘고(그건 문서 몇 개에만 있다) 진짜 참조가 늘었기 때문이다.
//
// # 무엇이 바뀌어서 가능해졌나 — 파서와 모양 필터
//
// 앞선 측정이 오탐 15개를 본 것은 검사기가 순진했기 때문이기도 하다. 이 구현은
// 세 가지를 더 한다:
//
//  1. **코드 펜스를 센다.** ```toml 안의 `[[domain]]` 은 TOML array-of-tables 다.
//  2. **별칭 파이프를 끊는다. 표 안에서는 `\|` 로 이스케이프된다** — 이걸 놓치면
//     `[[대상\|별칭]]` 이 `대상\` 로 잡혀 **멀쩡한 링크가 깨진 것으로 보인다.**
//     실측 중에 이 실수를 실제로 했고, 그것만으로 후보가 68개에서 28개로 줄었다.
//  3. **모양 필터**: 이 볼트의 이름 규약에 맞는 것만 남긴다(아래 looksLikeNote).
//
// 3번이 결정적이다. 남은 잡음은 전부 자리표시자인데, 그것들은 규약을 안 따른다 —
// `[[X]]`·`[[wikilink]]`·`[[source]]`·`[[T-...]]`·`[[벧전 5:7]]`. 반대로 진짜
// 참조는 예외 없이 규약을 따른다. 그래서 낱말 목록(blocklist)이 아니라 모양으로 가른다.
// 목록은 새 자리표시자가 생길 때마다 늘어나지만 모양은 안 늘어난다.
//
// # 왜 지우지 않고 말만 하는가
//
// 본문은 사람이 쓴 문장이다. `related` 처럼 빼 버릴 수 있는 필드가 아니다 —
// "근거 = [[X]]" 에서 링크를 빼면 문장이 거짓이 된다. 그래서 **알리고 사람이 고친다.**
//
// # 두 종류를 갈라 말한다
//
// **가까운 이름이 있는 것**은 오타·옛 규약·날짜 어긋남이다. 고칠 수 있다.
//
// **가까운 이름이 없는 것**은 다르다 — 참조된 결정이 **애초에 안 쓰였다.**
// 2026-08-27 실볼트에 셋 있었다(`draft00-결정-업계벤치마크-반증검증필수`,
// `draft00-결정-로고월대신타입마퀴-2026-08-10`,
// `nova-결정-문개구부는-바닥두께에서-역산한다-2026-08-14`). 문서는 "이건 저기서
// 정했다" 고 말하는데 그 저기가 없다. 고칠 대상이 링크가 아니라 **없는 기록**이라,
// 같은 줄에 섞으면 사람이 오타로 보고 이름만 고치려 든다.
func checkBodyLinks(r *Report, l *store.Layout) {
	stems, err := l.AllStems()
	if err != nil {
		return // 볼트를 못 훑으면 다른 검사가 이미 크게 말한다
	}
	typo, orphan := scanBodyLinks(l.Vault(), stems)
	if len(typo) == 0 && len(orphan) == 0 {
		r.add("본문 링크", OK, "본문 위키링크가 전부 실재하는 문서를 가리킨다", "")
		return
	}
	var parts []string
	fix := ""
	if len(typo) > 0 {
		var names []string
		for _, d := range typo {
			names = append(names, fmt.Sprintf("%s → %s", d.Target, d.Suggest))
		}
		sort.Strings(names)
		parts = append(parts, fmt.Sprintf("이름이 어긋난 것 %d건 (%s)",
			len(typo), strings.Join(clip(names), " · ")))
		fix = "본문의 [[이름]] 을 제안된 이름으로 고쳐라 — **제안은 확인 대상이다**"
	}
	if len(orphan) > 0 {
		var names []string
		for _, d := range orphan {
			names = append(names, d.Target)
		}
		sort.Strings(names)
		parts = append(parts, fmt.Sprintf("가리키는 기록이 아예 없는 것 %d건 (%s)",
			len(orphan), strings.Join(clip(names), " · ")))
		if fix != "" {
			fix += " · "
		}
		fix += "이쪽은 오타가 아니라 **약속된 결정이 안 쓰인 것**이다 — 쓰거나, 링크를 걷어라"
	}
	r.add("본문 링크", Warn, strings.Join(parts, " · "), fix)
}

// BodyLink 는 본문에서 대상을 못 찾은 위키링크 하나다.
type BodyLink struct {
	Path    string // 볼트 기준 상대 경로
	Target  string // [[ ]] 안의 이름
	Suggest string // 가장 가까운 실재 이름. 없으면 빈 문자열
}

var (
	wikiLink  = regexp.MustCompile(`\[\[(.+?)\]\]`)
	codeFence = regexp.MustCompile("^\\s*(```|~~~)")
	// dated 는 규약의 날짜 꼬리다 (`…-2026-08-12`).
	dated = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	// marker 는 결정 노트 표식이다. 설정에서 유도하지 않고 둘 다 받는다 —
	// 볼트에 두 규약이 섞여 있고(2026-08-21 합치기 전 `-decision-`), 옛 규약으로
	// 쓰인 링크를 잡는 것이 이 검사의 목적 중 하나다.
	marker = regexp.MustCompile(`-(결정|decision)-`)
	// numbered 는 `00-`·`14-` 로 시작하는 문서 이름이다.
	numbered = regexp.MustCompile(`^\d{2}-`)
	// inlineCode 는 백틱으로 감싼 조각이다. 옵시디언은 그 안의 `[[X]]` 를 링크로
	// 만들지 않으므로 우리도 세면 안 된다 — 스키마·규약 문서가 자리표시자를 그렇게
	// 적는다(`superseded-by: # 뒤집혔을 때 \`[[새-결정-노트]]\` 를 넣는다`).
	//
	// 펜스만 보고 인라인을 안 보면 그런 문서가 **영원히 짖는다.** 늘 뜨는 경고는
	// 무시하는 법을 가르치고, 그러면 진짜 깨진 링크도 같이 묻힌다.
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// looksLikeNote 는 **볼트 노트 이름으로 쓰려던 것인지** 본다.
//
// 자리표시자를 낱말 목록으로 거르지 않는 이유: 목록은 새 자리표시자가 생길 때마다
// 늘어나고, 늘리는 것을 잊으면 조용히 오탐이 된다. 규약은 안 늘어난다.
func looksLikeNote(v string) bool {
	return dated.MatchString(v) || marker.MatchString(v) || numbered.MatchString(v)
}

// linkTarget 은 `[[ ]]` 안에서 **대상 이름**만 뽑는다.
//
// 별칭은 `|` 로 나뉘는데 **표 안에서는 `\|` 로 이스케이프된다.** 그걸 안 끊으면
// 대상 이름 끝에 역슬래시가 붙어 멀쩡한 링크가 전부 깨진 것으로 보인다.
// 헤딩 앵커(`#`)와 블록 참조도 잘라 낸다.
func linkTarget(raw string) string {
	v := raw
	if i := strings.Index(v, `\|`); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '|'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v = v[:i]
	}
	return store.NFC(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(v), `\`)))
}

func scanBodyLinks(vault string, stems map[string]bool) (typo, orphan []BodyLink) {
	_ = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 못 읽는 자리는 건너뛴다 — 이 검사는 곁다리다
		}
		if d.IsDir() {
			switch d.Name() {
			case ".obsidian", ".git", ".trash", "_derived":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		body := string(b)
		// frontmatter 는 checkLinks 의 몫이다. 두 검사가 같은 것을 두 번 말하면
		// 사람이 같은 링크를 두 줄에서 보고 어느 쪽을 고칠지 헷갈린다.
		if strings.HasPrefix(body, "---\n") {
			if e := strings.Index(body[4:], "\n---"); e >= 0 {
				body = body[4+e+4:]
			}
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, vault), string(filepath.Separator))
		inFence := false
		for _, line := range strings.Split(body, "\n") {
			if codeFence.MatchString(line) {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			// 인라인 코드를 먼저 지운다 (위 inlineCode 주석).
			for _, m := range wikiLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				v := linkTarget(m[1])
				if v == "" || stems[v] || !looksLikeNote(v) {
					continue
				}
				link := BodyLink{Path: rel, Target: v, Suggest: store.NearestStem(v, stems)}
				if link.Suggest == "" {
					orphan = append(orphan, link)
				} else {
					typo = append(typo, link)
				}
			}
		}
		return nil
	})
	return typo, orphan
}
