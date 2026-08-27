package health

import (
	"fmt"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// ruleSummaryRunes 는 규칙 요약이 이 길이를 넘으면 말해 준다.
//
// **규칙의 값은 짧다는 것이다.** 회수 자리가 두 개뿐이고 매 프롬프트에 실리므로
// 긴 규칙은 그 자리에서 예산만 먹는다. 그리고 요약이 길어지면 그건 규칙이 아니라
// 결정이다 — 사건 서술이 섞여 들어간 것이고, 그러면 다른 프로젝트에서 안 걸린다.
//
// 200 은 회수 점수식의 길이 정규화 기준(search.refHeadRunes)과 같은 수다. 그 위로
// 가면 head 히트가 실제로 감점되기 시작한다 — 즉 이 경계는 취향이 아니라
// 점수식이 정한 것이다. 상수를 import 하지 않고 적어 두는 이유는 health 가
// search 의 내부 상수에 의존하면 안 되기 때문이고, 어긋나도 손해는 경고 문구
// 하나뿐이다.
const ruleSummaryRunes = 200

// checkRules 는 **규칙 노트**의 상태를 말한다 (store/rule.go).
//
// 이 검사가 있는 이유는 동의어 표와 같다.
//
// **① 발견 표면.** 규칙은 폴더 하나를 만드는 것으로 켜진다. 설정 키도 명령도
// 없으니 doctor 가 유일한 입구다. 폴더가 없는 것은 고장이 아니므로 경고가
// 아니라 사실로 적는다.
//
// **② 조용히 빠진 규칙을 보이게 한다.** 규칙은 몇 건뿐이라 한 건이 안 읽히는
// 손해가 결정보다 크다. 결정 414건 중 하나가 빠지면 회수가 조금 좁아지지만,
// 규칙 6건 중 하나가 빠지면 그 판단 기준이 전 프로젝트에서 사라진다.
//
// **③ 근거 없는 규칙을 보이게 한다.** 규칙은 결정에서 증류한 것이라 `related` 에
// 출처 결정이 있어야 한다. 없으면 다음 사람이 그것을 신뢰할지 판단할 수 없고,
// 판단할 수 없는 규칙은 지워지지도 않고 영원히 남는다 — 동의어 표가 "고칠 때
// 이유를 남겨라" 로 같은 병을 막는 자리다.
func checkRules(r *Report, l *store.Layout) {
	rules, skipped, err := l.ListRules()
	if err != nil {
		r.add("규칙", Warn, fmt.Sprintf("규칙 폴더를 읽을 수 없다: %v", err),
			"폴더 권한을 보라. 규칙은 도메인이 없어 전 프로젝트 회수에 걸리는 유일한 계층이다")
		return
	}
	if len(skipped) > 0 {
		names := make([]string, 0, len(skipped))
		for _, s := range skipped {
			names = append(names, l.RelPath(s.Path))
		}
		r.add("규칙", Warn,
			fmt.Sprintf("규칙 %d건을 읽었지만 %d건을 읽지 못했다 — %s",
				len(rules), len(skipped), strings.Join(clip(names), " · ")),
			"frontmatter 가 깨졌다. 규칙은 몇 건뿐이라 한 건이 빠지면 그 판단 기준이 "+
				"전 프로젝트에서 사라진다")
		return
	}
	if len(rules) == 0 {
		r.add("규칙", OK,
			fmt.Sprintf("규칙이 없다 — 결정에 묻힌 판단 기준은 다른 프로젝트에서 회수되지 않는다 (%s/)",
				l.RulesDirRel()),
			"")
		return
	}

	var noProvenance, tooLong []string
	for _, n := range rules {
		if len(n.Meta.Related) == 0 && len(n.Meta.Supersedes) == 0 {
			noProvenance = append(noProvenance, n.Stem)
		}
		if len([]rune(n.Meta.Summary)) > ruleSummaryRunes {
			tooLong = append(tooLong, n.Stem)
		}
	}
	if len(noProvenance) > 0 {
		r.add("규칙", Warn,
			fmt.Sprintf("규칙 %d건 중 %d건에 출처 결정이 없다 — %s",
				len(rules), len(noProvenance), strings.Join(clip(noProvenance), " · ")),
			"그 규칙의 `related` 에 근거가 된 결정을 위키링크로 걸어라. 근거 없는 규칙은 "+
				"다음 사람이 신뢰할지 판단할 수 없어서 지워지지도 않고 영원히 남는다")
		return
	}
	if len(tooLong) > 0 {
		r.add("규칙", Warn,
			fmt.Sprintf("규칙 %d건 중 %d건의 요약이 %d자를 넘는다 — %s",
				len(rules), len(tooLong), ruleSummaryRunes, strings.Join(clip(tooLong), " · ")),
			"규칙의 값은 짧다는 것이다. 이 길이를 넘으면 회수 점수식의 길이 정규화가 "+
				"감점을 시작하고, 무엇보다 사건 서술이 섞였다는 뜻이다 — 그러면 다른 "+
				"프로젝트에서 안 걸린다")
		return
	}
	r.add("규칙", OK,
		fmt.Sprintf("%d건 — 도메인이 없어 어느 프로젝트에서 물어도 걸린다", len(rules)), "")
}
