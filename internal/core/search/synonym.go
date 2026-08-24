package search

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// 동의어 다리 — **같은 질문을 바꿔 말했을 때도 걸리게 한다.**
//
// # 고치려는 고장
//
// 회수는 낱말이 겹치는지만 본다. 그래서 개념이 볼트에 있는데도 다른 낱말로 물으면
// 0건이 난다. [[priorcase-결정-PARA기각-폴더가아니라어휘가병목이다-2026-08-23]] 가
// 그것을 실측으로 못 박았고, 2026-08-24 에 그대로 재현됐다:
//
//	"웹소설을 AI로 여러 편 찍어내면 경쟁력이 있나"  → 결정 7건 (정답 1위)
//	"작품을 많이 만들어서 승부하면 되나"            → 결정 0건   ← 같은 질문
//	"불러오는 느낌이 안 든다"                       → 8건 전부 무관
//	"회수"                                          → 정답
//
// # 왜 태그로는 안 되는가
//
// 태그는 **노트 쪽**을 넓히는 수단이라 노트마다 사람이 적어야 하고, 패러프레이즈는
// 무한하다. 2026-08-24 에 헛도는 태그 13건을 손으로 고쳐 0건으로 만들었지만 위
// 두 번째 질의는 그대로 0건이었다 — 293건의 다른 노트에 같은 낱말을 다 심을 수는
// 없다. 동의어 표는 **질의 쪽**을 넓혀서 노트를 하나도 안 고치고 전건에 걸린다.
//
// # 왜 임베딩이 아닌가
//
// PARA기각 결정문이 임베딩을 "패러프레이즈를 실제로 잡는 유일한 해법" 으로 적고
// 로컬 모델·색인 부담과 "API 키는 쓰지 않는다" 제약 때문에 보류했다. 그 대안 목록에
// **동의어 표가 없었다** — 태그(약함)에서 임베딩(무거움)으로 바로 건너뛰었다.
// 이건 그 사이다: 의존성 0, 색인 0, 파일 하나.
//
// # 왜 설정이 아니라 볼트에 두는가
//
// **설정은 머신 사이를 건너오지 않는다.** 2026-08-24 실측: 집에서 만든 novels·tutela
// 도메인이 `config.toml` 에 없어 결정 23건이 회사 머신에서 통째로 안 보였다
// ([[priorcase-결정-두머신분기-병합-스키마갈림이데이터를덮었다-2026-08-22]] 의 예상
// 리스크가 그대로 실현된 것이다). 볼트는 git 으로 오간다. 그리고 이 낱말들은
// 프로그램의 것이 아니라 **쓰는 사람의 것**이라 옵시디언에서 바로 고칠 수 있어야 한다.
//
// 경로를 설정 키로 빼지 않고 박아 둔 이유도 같다 — 키로 빼면 그 키가 또 안 건너온다.
//
// # 어떻게 세는가
//
// 질의어 하나가 정확히 맞으면 head 히트(3점), 정확히는 안 맞고 **형제 낱말**이
// 맞으면 동의어 히트(1점)다. 한 질의어는 **최대 한 번** 센다 — 형제가 다섯이라고
// 다섯 배가 되면 표를 크게 쓴 사람이 이긴다.
//
// 1점인 이유: 큐레이션된 패러프레이즈가 head 에 걸린 것은 정확히 맞은 것보다는
// 약하고, 본문에 정확히 걸린 것(weightBody=1)과 비슷한 세기의 증거다. 그래서
// **정확히 맞은 노트가 언제나 패러프레이즈로 맞은 노트를 앞선다.**
//
// # minHeadHits 게이트를 통과시키는 이유
//
// 그 게이트는 CJK 부분문자열 매칭이 만드는 **우연한** 히트를 막으려고 있다
// (scoreAll 주석의 실측). 동의어 히트는 우연이 아니다 — 사람이 그 낱말을 표에
// 직접 적었을 때만 난다. 즉 **큐레이션이 필터다.** 그래서 게이트에는 온전히 세고,
// 대신 표에 흔한 낱말이 들어가는 것이 이 설계의 실패 모드다. `prior doctor` 가
// 불용어·짧은 낱말을 거절하고 그 사실을 말한다.
//
// # 파일이 없으면 아무것도 달라지지 않는다
//
// 볼트에 표가 없으면 빈 표로 돌고 점수 계산이 지금과 한 바이트도 다르지 않다.
// 켜는 것은 파일을 만드는 행위 하나다.

// synonymFiles 는 볼트에서 찾아 볼 표 파일이다. lang 에 맞는 쪽을 먼저 본다.
var synonymFiles = map[string][]string{
	"ko": {"_meta/00-회수-동의어.md", "_meta/00-recall-synonyms.md"},
	"en": {"_meta/00-recall-synonyms.md", "_meta/00-회수-동의어.md"},
}

// Synonyms 는 낱말 하나에서 형제 낱말들로 가는 표다. 제로값이 유효하다 (빈 표).
type Synonyms struct {
	sib    map[string][]string
	groups int
	// Rejected 는 표에 적혔지만 받지 않은 낱말이다 — 불용어이거나 너무 짧다.
	// doctor 가 이것을 사람에게 말한다. 조용히 버리면 "적었는데 안 걸린다" 가 된다.
	Rejected []string
}

// Groups 는 받아들인 동의어 묶음 수다.
func (s Synonyms) Groups() int { return s.groups }

// Empty 는 표가 비었는지다 — 비었으면 점수 계산이 동의어 없던 때와 같다.
func (s Synonyms) Empty() bool { return len(s.sib) == 0 }

// LoadSynonyms 는 볼트에서 동의어 표를 읽는다.
//
// **파일이 없는 것은 에러가 아니다.** 대다수 볼트에 이 파일이 없고, 없는 것이
// 정상이다. 읽을 수 없는 것(권한 등)도 회수를 죽이지 않는다 — 회수가 조금 좁아질
// 뿐이고, 그 사실은 doctor 가 따로 말한다.
func LoadSynonyms(l *store.Layout) Synonyms {
	if l == nil {
		return Synonyms{}
	}
	names := synonymFiles[string(l.Lang())]
	if names == nil {
		names = synonymFiles["ko"]
	}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(l.Vault(), filepath.FromSlash(name)))
		if err != nil {
			continue
		}
		return parseSynonyms(string(b))
	}
	return Synonyms{}
}

// SynonymPath 는 표가 있어야 할 자리다 (doctor 가 사람에게 알려 줄 때 쓴다).
func SynonymPath(l *store.Layout) string {
	names := synonymFiles[string(l.Lang())]
	if names == nil {
		names = synonymFiles["ko"]
	}
	return names[0]
}

// parseSynonyms 는 마크다운 목록에서 표를 만든다.
//
// 한 줄이 한 묶음이다: `- 회수, 불러오기, 검색`. 목록이 아닌 줄(제목·설명·빈 줄)은
// 전부 무시한다 — 그래야 사람이 그 파일에 이유를 적을 수 있다. 이유가 적히지 않는
// 파일은 왜 그 낱말을 넣었는지 잊혀지고, 그러면 아무도 못 고친다.
func parseSynonyms(src string) Synonyms {
	s := Synonyms{sib: map[string][]string{}}
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "* ") {
			continue
		}
		var words []string
		for _, w := range strings.Split(t[2:], ",") {
			w = normalizeSynonym(w)
			if w == "" {
				continue
			}
			if len([]rune(w)) < minTokenRunes || stopwords[w] {
				// **거절을 보이게 한다.** 적었는데 안 걸리는 것이 이 기능의
				// 최악 실패다 — 사람은 표를 고쳤다고 믿고 회수는 그대로다.
				s.Rejected = append(s.Rejected, w)
				continue
			}
			words = append(words, w)
		}
		if len(words) < 2 {
			continue // 혼자 있는 낱말은 아무것도 안 넓힌다
		}
		s.groups++
		for _, w := range words {
			for _, other := range words {
				if other != w && !containsStr(s.sib[w], other) {
					s.sib[w] = append(s.sib[w], other)
				}
			}
		}
	}
	return s
}

// normalizeSynonym 은 사람이 옵시디언에서 쓸 만한 장식을 벗긴다 — 백틱, 위키링크
// 대괄호, 강조 별표. 벗기지 않으면 `회수` 와 회수가 다른 낱말이 된다.
func normalizeSynonym(w string) string {
	w = strings.TrimSpace(w)
	w = strings.Trim(w, "`*_[]")
	return strings.ToLower(strings.TrimSpace(w))
}

// hits 는 질의어 k 의 형제 중 하나라도 text 에 있는지다.
//
// **형제는 정확히 하나만 센다** — 몇 개가 맞았는지는 안 본다. 표를 크게 쓴 낱말이
// 점수를 독식하지 않게 하는 유일한 장치다.
func (s Synonyms) hits(text, k string) bool {
	for _, sib := range s.siblings(k) {
		if matches(text, sib) {
			return true
		}
	}
	return false
}

// siblings 는 질의어의 형제를 준다. 정확히 없으면 **접두 일치**로 한 번 더 찾는다.
//
// # 왜 접두 일치가 필요한가
//
// 표는 낱말의 사전형을 담는데 질의는 활용형으로 온다. 실측(2026-08-24):
//
//	"많이 만들어서 승부"            → 토큰 `승부`     → 표에 맞음 → 정답 1위
//	"작품을 많이 만들어서 승부하면 되나" → 토큰 `승부하면` → 표에 없음 → 정답 탈락
//
// 같은 질문인데 문장으로 쓰면 못 찾는다. `ExtractKeywords` 는 조사를 떼지만
// 어미(하면·하는·했다·하기)는 떼지 않는다 — 그건 조사와 달리 낱말의 일부일 때가
// 많아서 함부로 떼면 다른 낱말이 된다. 그래서 여기서 흡수한다.
//
// # 왜 접두만 보는가 (부분문자열이 아니라)
//
// 한국어 활용은 **뒤에** 붙는다: 승부→승부하면, 물량→물량으로, 손절→손절했다.
// 그래서 "표의 낱말로 시작하는 질의어" 만 받으면 활용을 거의 다 잡으면서
// 우연을 안 만든다. 낱말 가운데를 보면 "대량" 이 "대량살상" 같은 무관한 토큰에
// 걸린다 — 그건 이 기능이 아니라 잡음이다.
//
// **반대 방향은 안 본다.** 질의어가 표의 낱말보다 짧을 때(`승부` vs 표의 `승부수`)
// 이어 주면, 짧은 질의어 하나가 표의 긴 낱말 여럿을 켜서 점수가 새어 나간다.
//
// 가장 긴 것 하나만 쓴다. 여러 낱말이 접두로 걸리면 더 구체적인 쪽이 뜻에 가깝다.
func (s Synonyms) siblings(k string) []string {
	if sib, ok := s.sib[k]; ok {
		return sib
	}
	best := ""
	for w := range s.sib {
		if len(w) < len(k) && strings.HasPrefix(k, w) && len(w) > len(best) {
			best = w
		}
	}
	if best == "" {
		return nil
	}
	return s.sib[best]
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
