package capture

import (
	"fmt"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// supersede 는 "이 결정이 저 결정을 뒤집는다" 를 양방향으로 엮는다.
// prior capture --supersedes 와 prior review --supersedes 가 이 함수 하나를 쓴다.
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
// **reason 이 네 번째다 (2026-08-19).** 그 전까지 이 함수의 시그니처에는 이유를
// 받는 인자가 아예 없었고, 옛 노트에 하는 일은 status="superseded" 와 related 에
// 위키링크 한 줄을 붙이는 것이 전부였다. 즉 **무엇이** 뒤집었는지는 남고 **왜**
// 뒤집혔는지는 한 글자도 안 남았다. 실측: 실볼트 18노트 중 번복 사유가 남은 것 0건.
// 사용자 정책(볼트 [[common-decision-결정기록-결론뿐아니라-대안기각이유-번복까지-2026-08-19]])이
// 요구하는 "중간에 틀렸다가 정정한 판단과 그 계기" 를 담을 자리가 없었던 것이다.
// reason 은 비어도 된다 — 옛 호출부(CLI·MCP)가 아직 안 넘기므로 강제하면
// 뒤집기 자체가 막힌다. 넘어오면 markOverturned 가 세 자리에 새긴다.
//
// 디스크에는 쓰지 않는다. 호출부가 새 노트까지 검증한 뒤에 쓰기를 시작해야
// 하기 때문이다 — 옛 노트를 먼저 쓰고 새 노트 검증에서 실패하면 옛 노트만
// superseded 로 남아 양방향 연결이 반쪽짜리 상태로 디스크에 고정된다.
//
// 준다: 새 노트의 supersedes 에 넣을 위키링크와, status·related·번복 사유를 갱신한 옛 노트.
func supersede(l *store.Layout, target, newStem, reason, date string) (string, store.Note, error) {
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
	back, err := store.NormalizeLink(newStem)
	if err != nil {
		return "", store.Note{}, fmt.Errorf("역링크를 만들 수 없다: %w", err)
	}
	fwd, err := store.NormalizeLink(old.Stem)
	if err != nil {
		return "", store.Note{}, fmt.Errorf("링크를 만들 수 없다: %w", err)
	}
	old.Meta.Status = "superseded"
	old.Meta.Related = appendUnique(old.Meta.Related, back)
	// **왜 뒤집혔는지도 남긴다.** 링크만 남기면 "무엇이" 는 알아도 "왜" 는 못 안다 —
	// 실볼트 18노트 중 번복 사유가 기록된 것이 0건이었다. reason 이 비면 아무 일도 안 한다.
	markOverturned(l, &old, reason, date, back)
	return fwd, old, nil
}

// supersedeAll 은 **여러 대상을 한 번에** 엮는다.
//
// 하나만 되던 시절에 실제로 데이터를 잃었다: 2026-08-13 `방향전환-개인도구-다중볼트`
// 가 전제 6개를 폐기 선언했는데 `--supersedes` 가 한 칸뿐이라 1건만 엮였고, 나머지는
// 본문 산문으로 밀려나 두 노트가 "superseded 인데 무엇이 뒤집었는지 없는" 상태로
// 남았다. 그 상태를 doctor 의 뒤집기 검사가 지금 문다.
//
// **번복 사유는 대상마다 같은 것을 쓴다.** 한 결정이 전제 여럿을 한꺼번에 걷어낼
// 때 그 이유는 하나이기 때문이다 — 대상별로 다른 이유가 필요하면 review 를 따로 부른다.
//
// 디스크에는 쓰지 않는다 — supersede 와 같은 이유다(호출부가 전부 검증한 뒤에 쓴다).
func supersedeAll(l *store.Layout, targets []string, newStem, reason, date string) (store.LinkList, []store.Note, error) {
	var links store.LinkList
	var olds []store.Note
	seen := map[string]bool{}
	for _, t := range targets {
		link, old, err := supersede(l, t, newStem, reason, date)
		if err != nil {
			return nil, nil, err
		}
		// 같은 대상을 두 번 주면 옛 노트를 두 번 쓰게 된다. 두 번째 쓰기는 첫 번째의
		// 결과를 못 보므로 related 가 어긋날 수 있다 — 여기서 접는다.
		if seen[old.Stem] {
			continue
		}
		seen[old.Stem] = true
		links = append(links, link)
		olds = append(olds, old)
	}
	return links, olds, nil
}

// normalizeLinks 는 사용자·에이전트가 준 링크 목록을 정본 형태로 접는다.
//
// **하나라도 나쁘면 통째로 거부한다.** 조용히 빼면 사용자는 링크를 걸었다고 믿는데
// 볼트에는 없다 — 이 프로젝트가 죄목으로 드는 "조용한 무동작" 이다.
func normalizeLinks(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		link, err := store.NormalizeLink(s)
		if err != nil {
			return nil, fmt.Errorf("related 가 잘못됐다: %w", err)
		}
		out = appendUnique(out, link)
	}
	return out, nil
}

// markOverturned 는 뒤집힌 노트에 **왜 뒤집혔는가** 를 새긴다. reason 이 비면 아무것도 안 한다.
//
// # 왜 세 자리인가 — 회수 구조가 강제한다
//
// 세 자리에 나눠 쓰는 것은 중복이 아니라 **독자가 셋이라서** 다.
//
//  1. frontmatter `superseded_reason` — 정본. 온전한 한 줄이 구조화된 자리에 남아
//     도구가 읽는다(색인·회고·나중의 렌더링). 사람이 손으로 고칠 수 있는 자리이기도 하다.
//
//  2. summary 꼬리표 — **회수에 실려 나가는 유일한 자리다.** core/search 의
//     scoreAll 은 head 를 `stem + summary + contentTags` 로만 만들고, head 히트가
//     0이면 `continue` 로 노트를 **통째로 버린다**(search.go:116). 즉 본문에만 이유를
//     적으면 그 낱말로는 그 결정이 영영 안 잡힌다 — bodyHits 가 아무리 많아도 소용없다.
//     frontmatter 새 키도 마찬가지다: head 에 안 들어가므로 검색되지 않는다.
//     게다가 훅 주입(RenderInject)이 노트당 내보내는 것도 summary 한 줄이라,
//     여기 없으면 에이전트는 "(superseded)" 라는 딱지만 보고 이유는 못 본다.
//     그래서 summary 에 붙인다 — 다른 선택지는 회수에 도달하지 못한다.
//
//  3. 회고 절 본문 — 줄바꿈·측정치·명령어가 든 **온전한 원문**. summary 는 한 줄이라
//     길이를 잘라야 하는데, 자른 부분이 사라지면 안 된다.
//
// 절을 새로 만들지 않고 기존 회고 절에 붙이는 이유: 사용자 정책이 번복을
// "outcome·retrospective 를 갱신하거나 supersedes 로 대체한다" 로 묶어 놓았고,
// appendRetrospective 가 이미 두 언어 절 제목·뒤따르는 절 처리·삽입 위치를 다 푼다.
// 절을 하나 더 만들면 그 로직이 두 벌이 된다.
func markOverturned(l *store.Layout, n *store.Note, reason, date, byLink string) {
	full := strings.TrimSpace(reason)
	if full == "" {
		return
	}
	line := oneLine(full)

	// frontmatter 에는 접힐 수 없는 한 줄만 넣는다. store.quote 는 emitter 가
	// 스칼라를 여러 줄로 접으면 panic 한다 — 여기서 줄바꿈을 흡수하지 않으면
	// 여러 줄짜리 이유 하나가 방출기를 죽인다.
	n.Meta.SupersededReason = line

	n.Meta.Summary = stripOverturnMark(n.Meta.Summary) +
		overturnMark(n.Body, l.Lang()) + clipRunes(line, summaryReasonRunes)

	var body string
	if byLink != "" {
		body = fmt.Sprintf(l.Lang().T(
			"%s: 이 결정은 %s 로 뒤집혔다. 이유: %s",
			"%s: Overturned by %s. Reason: %s"), date, byLink, full)
	} else {
		// 대체할 새 결정 없이 뒤집는 경우다 — "그냥 그만둔다" 로 끝나는 번복.
		body = fmt.Sprintf(l.Lang().T(
			"%s: 이 결정을 번복한다. 이유: %s",
			"%s: This decision is overturned. Reason: %s"), date, full)
	}
	n.Body = appendRetrospective(n.Body, body, l.Lang())
}

// summaryReasonRunes 는 summary 꼬리표에 들어갈 이유의 최대 길이다.
//
// 실측으로 정했다: 실볼트 18개 summary 의 길이는 39~111자, 중앙값 60자다. 꼬리표를
// 80자로 자르면 최악의 줄이 111+80 ≈ 190자가 되는데, 회수는 노트 3건을 주입하므로
// 주입 블록이 600자를 넘지 않는다. 무제한으로 두면 이유 하나가 회수 블록을 통째로
// 잡아먹어, 번복을 기록하려다 회수를 망친다. 잘린 나머지는 안 잃는다 —
// frontmatter 정본과 회고 본문에 온전히 남는다.
const summaryReasonRunes = 80

// overturnMark 는 이 노트에 붙일 꼬리표를 고른다. **그 노트의 언어를 따라간다** —
// retroHeading 과 같은 규칙이고, 그 함수와 같은 englishSectionRe 를 단서로 쓴다.
//
// 리터럴을 여기와 overturnMarks 에 두 번 쓴다. retroHeading / retroHeadRe 와 같은
// 모양이고, 이유도 같다: internal/arch 의 TestTranslationPairsHaveMatchingVerbs 가
// T() 인자를 **상수 리터럴로만** 접을 수 있다(비상수 포맷 문자열은 go vet 의 printf
// 검사가 통째로 꺼지는 자리라, 그 테스트가 안전망을 대신한다). 상수 식별자를 넘기면
// 검사기가 못 읽고 실패한다. 어긋남은 주석이 아니라 테스트로 막는다 —
// TestOverturnMarkIsStrippable 가 두 언어 모두에서 붙였다 떼는 왕복을 확인한다.
func overturnMark(body []byte, lang i18n.Lang) string {
	if englishSectionRe.Match(body) {
		return " — Overturned: "
	}
	return lang.T(" — 번복: ", " — Overturned: ")
}

// overturnMarks 는 떼어낼 때 알아보는 표식들이다. **두 언어를 다 알아본다** —
// retroHeadRe 가 두 언어 절 제목을 다 아는 것과 같은 이유다. 볼트에 두 언어가
// 섞이면(사용자가 lang 을 바꿨거나 팀이 볼트를 공유하거나) 한쪽만 아는 코드가
// 꼬리표를 겹쳐 붙여 summary 가 번복 이유로 도배된다.
var overturnMarks = []string{" — 번복: ", " — Overturned: "}

// stripOverturnMark 는 이미 붙어 있는 번복 꼬리표를 떼어낸다.
//
// 같은 노트에 꼬리표를 두 번 붙이는 일은 실제로 일어난다 — 이유를 잘못 적어
// review 로 다시 쓰는 경우, 그리고 뒤집힌 노트를 또 다른 결정이 뒤집는 경우.
// 떼지 않고 붙이면 summary 가 꼬리표 사슬이 되어 회수에 실리는 한 줄을 못 읽게 된다.
func stripOverturnMark(summary string) string {
	cut := -1
	for _, mark := range overturnMarks {
		if i := strings.Index(summary, mark); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut < 0 {
		return summary
	}
	return strings.TrimRight(summary[:cut], " ")
}

// oneLine 은 줄바꿈·탭을 공백으로 접고 연속 공백을 하나로 줄인다.
// frontmatter 스칼라와 summary 는 둘 다 한 줄이어야 한다.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clipRunes 는 룬 단위로 자른다. 바이트로 자르면 한글이 반 토막 나 깨진 글자가 남는다.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n]), " ") + "…"
}
