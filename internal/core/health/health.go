// Package health 는 "지금 priorcase 가 제대로 돌고 있는가" 를 검사한다.
//
// **이 패키지의 존재 이유는 조용한 무동작이다.** 이 시스템의 부품은 전부 실패해도
// 대화를 막지 않도록 설계됐다 — 훅은 무슨 일이 있어도 exit 0 이고, 회수는 못 찾으면
// 아무것도 안 내고, 데몬은 백그라운드다. 그 설계의 대가로 **고장이 정상과 구별되지
// 않는다.** 여기가 그걸 구별하는 유일한 자리다.
//
// core 만으로 할 수 있는 검사를 모은다. 훅 배선·데몬 상태처럼 호스트나 프로세스를
// 알아야 하는 검사는 어댑터가 자기 것을 덧붙인다 (§4.1).
package health

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/schema"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/core/sync"
	"github.com/xian0310567/priorcase/internal/core/xdgpath"
)

// Level 은 검사 결과의 심각도다.
type Level int

const (
	OK   Level = iota // 정상
	Warn              // 동작하지만 손해가 있다
	Fail              // 동작하지 않는다
)

func (l Level) Mark() string {
	switch l {
	case OK:
		return "✓"
	case Warn:
		return "⚠"
	}
	return "✗"
}

// Check 는 검사 하나의 결과다.
//
// Fix 를 따로 두는 이유: 진단만 하고 무엇을 하라는 말이 없으면 사용자는 그 경고를
// 무시하는 법을 배운다. 고칠 방법을 모르면 고칠 수 없다.
type Check struct {
	Name   string
	Level  Level
	Detail string
	Fix    string
}

// Report 는 검사 묶음이다.
type Report struct{ Checks []Check }

func (r *Report) add(name string, lv Level, detail, fix string) {
	r.Checks = append(r.Checks, Check{Name: name, Level: lv, Detail: detail, Fix: fix})
}

// Worst 는 가장 나쁜 등급이다. 호출자가 종료 코드로 옮긴다.
func (r *Report) Worst() Level {
	worst := OK
	for _, c := range r.Checks {
		if c.Level > worst {
			worst = c.Level
		}
	}
	return worst
}

// Vault 는 볼트와 설정에 대한 검사를 돈다.
func Vault(c *config.Config, l *store.Layout) *Report {
	r := &Report{}
	checkVaultDir(r, c)
	checkStaleKeys(r, c, l)
	checkDomainFolders(r, c, l)
	checkUndeclared(r, l)
	checkUnscanned(r, c, l)
	notes := checkNotes(r, l)
	checkSchema(r, l, notes)
	checkSimilarSlugs(r, notes)
	checkLinks(r, l, notes)
	checkSupersedeSymmetry(r, notes)
	checkVocabulary(r, notes)
	checkSynonyms(r, l)
	checkSync(r, c)
	checkBuildDrift(r, c, sync.ThisBuild())
	return r
}

// RecentDecisions 는 최근 days 일 안에 날짜가 찍힌 결정 수다.
//
// 컷오버 회고의 한쪽 축이다 — 다른 쪽은 미확인 구간 수이고, 둘을 나란히 놓아야
// "기록이 되고 있는가" 를 판정할 수 있다.
func RecentDecisions(l *store.Layout, now time.Time, days int) int {
	notes, _, err := l.List()
	if err != nil {
		return -1
	}
	cut := now.AddDate(0, 0, -days).Format("2006-01-02")
	n := 0
	for _, note := range notes {
		if note.Meta.Date >= cut {
			n++
		}
	}
	return n
}

// allStems 는 볼트의 모든 마크다운 파일명(확장자 없이)을 NFC 로 접어 준다.
//
// 위키링크는 경로가 아니라 **파일명**으로 풀리므로(볼트 규약 문서의 실측) 대조도
// 파일명으로 한다. `.obsidian` 은 앱 설정이고 `.trash` 는 지운 것이라 뺀다.
func allStems(l *store.Layout) map[string]bool {
	out := map[string]bool{}
	root := l.Vault()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 못 읽는 자리는 건너뛴다 — 링크 검사는 곁다리다
		}
		if d.IsDir() {
			switch d.Name() {
			case ".obsidian", ".git", ".trash", "_derived":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			out[store.NFC(strings.TrimSuffix(d.Name(), ".md"))] = true
		}
		return nil
	})
	return out
}

// clip 은 진단 줄이 화면을 넘기지 않게 앞 몇 개만 보여 준다.
func clip(ss []string) []string {
	const n = 4
	if len(ss) <= n {
		return ss
	}
	return append(ss[:n:n], fmt.Sprintf("… 그 밖 %d건", len(ss)-n))
}

// checkSchema 는 **`prior capture` 가 거부했을 노트를 찾는다.**
//
// 파싱과 검증은 다르다. List() 는 frontmatter 가 10키인지만 보고, 접두어와 domain
// 첫 값이 같은지·status/outcome 이 허용값인지·날짜 형식이 맞는지는 안 본다.
// 손으로 쓴 노트는 이 검증을 통째로 우회하므로, 여기가 그 그물이다.
func checkSchema(r *Report, l *store.Layout, notes []store.Note) {
	var bad []string
	future := 0
	for _, n := range notes {
		// **더 새 판으로 쓰인 노트는 결함이 아니다.** 팀이 볼트를 공유하면 한 명이
		// 먼저 올린 상태가 정상이다. 그걸 "깨졌다" 로 보고하면 사용자가 멀쩡한
		// 남의 결정을 고치거나 지운다.
		if schema.IsFuture(n.Meta) {
			future++
			continue
		}
		if err := schema.Validate(l.DecisionMarker(), n.Stem, n.Meta); err != nil {
			bad = append(bad, fmt.Sprintf("%s (%v)", n.Stem, err))
		}
	}

	note := ""
	if future > 0 {
		note = fmt.Sprintf(" · 더 새 판으로 쓰인 노트 %d건 (읽기는 되지만 고칠 수 없다)", future)
	}
	if len(bad) == 0 {
		lv, fix := OK, ""
		if future > 0 {
			// 경고다. 지금 바이너리로는 그 노트를 갱신할 수 없으니 사람이 알아야 한다.
			lv, fix = Warn, "prior 를 올려라 — 팀원 중 누가 먼저 올렸다"
		}
		r.add("스키마", lv, fmt.Sprintf("%d건 전부 통과%s", len(notes)-future, note), fix)
		return
	}
	sort.Strings(bad)
	r.add("스키마", Fail, strings.Join(bad, " · ")+note,
		"prior capture 를 거치면 애초에 거부된다. 손으로 고치거나 다시 만들어라")
}

// checkSimilarSlugs 는 하이픈·공백·밑줄·대소문자만 다른 결정을 찾는다.
//
// 감사 결함 4 다 — 옛 구현의 유일한 방어가 "완전히 동일한 파일명이면 스킵" 이라
// slug 가 한 글자만 달라도 중복 노트가 생겼다. `prior capture` 는 이걸 거부하지만
// **손으로 쓰면 우회된다.**
func checkSimilarSlugs(r *Report, notes []store.Note) {
	groups := map[string][]string{}
	for _, n := range notes {
		groups[slugKey(n.Stem)] = append(groups[slugKey(n.Stem)], n.Stem)
	}
	var dups []string
	for _, v := range groups {
		if len(v) > 1 {
			sort.Strings(v)
			dups = append(dups, strings.Join(v, " ↔ "))
		}
	}
	if len(dups) == 0 {
		r.add("유사 slug", OK, "없다", "")
		return
	}
	sort.Strings(dups)
	r.add("유사 slug", Warn, strings.Join(dups, " · "),
		"둘 중 하나를 지우거나 prior review --supersedes 로 엮어라")
}

// checkBuildDrift 는 **다른 머신이 더 새 코드로 도는지** 본다.
//
// 2026-08-21 사고가 여기서 막힌다. 집이 supersedes 를 다중값으로 올렸고 회사는 옛
// 판이었는데, **아무도 그 사실을 몰랐다.** 노트가 안 읽히고 나서야 드러났고 그때는
// 이미 사람이 손댈 준비가 된 뒤였다 — 실제로 옛 모양으로 되돌려 데이터를 강등시켰다.
//
// **schema.Current 로는 못 잡는다.** 그건 사람이 올려야 하고, 이번 사고가 정확히
// 안 올려서 났다. 대신 Go 가 자동으로 박는 커밋 시각을 쓴다 — 아무도 기억할 필요가 없다.
//
// 조용해야 할 때: 내가 최신일 때, 도장이 없을 때(이 기능 이전 볼트), 빌드 정보가
// 없을 때(go run). 늘 뜨는 경고는 무시하는 법을 가르친다.
func checkBuildDrift(r *Report, c *config.Config, self sync.Build) {
	if c == nil {
		return
	}
	var lines []string
	for _, v := range c.Vaults {
		for _, b := range sync.NewerBuilds(v.Path, self) {
			age := b.Committed.Format("2006-01-02")
			mark := ""
			if b.Modified {
				// 커밋 안 된 변경 위에서 빌드했다 — revision 이 실제 코드를 안 가리킨다.
				mark = " (커밋 안 된 변경 포함)"
			}
			lines = append(lines, fmt.Sprintf("%s 가 더 새 판이다 (%s%s)", b.Host, age, mark))
		}
	}
	if len(lines) == 0 {
		r.add("판", OK, "이 머신이 최신이거나 비교할 것이 없다", "")
		return
	}
	sort.Strings(lines)
	r.add("판", Warn, strings.Join(lines, " · "),
		"prior 를 올려라 — 저쪽이 쓴 노트를 이 판이 못 읽을 수 있고, "+
			"그때 손으로 고치면 저쪽이 쌓은 것을 지운다")
}

// checkSync 는 **동기화가 조용히 죽어 있는지** 본다.
//
// 훅은 실패해도 대화를 막지 않는다(설계). 그 대가로 회사망에서 push 가 일주일간
// 막혀 있어도 아무도 모른다 — 이 프로젝트가 계속 경계해 온 "조용한 무동작" 이
// 정확히 여기다.
//
// # 왜 시각이 아니라 상태인가
//
// "마지막 동기화가 N일 전" 은 며칠 안 썼을 때 거짓 경고가 된다. 반면 **쓴 것이
// 안 밀린 것**은 언제 그랬든 손해다 — 다른 머신에서 그 결정이 안 보이고,
// 그러면 같은 것을 다시 정한다. 그래서 지금 상태를 직접 본다.
//
// **리모트가 없으면 경고하지 않는다.** 한 머신에서만 쓰는 볼트가 그렇고,
// 그건 고장이 아니라 설정이다. 매번 뜨는 경고는 무시하는 법을 가르친다.
func checkSync(r *Report, c *config.Config) {
	if c == nil {
		return
	}
	var warn []string
	used := 0
	for _, v := range c.Vaults {
		st := sync.Status(v.Path)
		if !st.HasRemote {
			continue
		}
		used++
		var parts []string
		if st.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("커밋 %d개", st.Ahead))
		}
		if st.Dirty > 0 {
			parts = append(parts, fmt.Sprintf("파일 %d개", st.Dirty))
		}
		if len(parts) > 0 {
			// 볼트가 하나면 이름을 안 붙인다. "default: …" 은 이름이 아니라 잡음이다.
			label := v.Name + ": "
			if len(c.Vaults) == 1 {
				label = ""
			}
			warn = append(warn, label+strings.Join(parts, " · ")+" 안 밀렸다")
		}
	}
	if used == 0 {
		r.add("동기화", OK, "리모트가 없다 — 이 머신에서만 쓴다", "")
		return
	}
	// 마지막 시도가 실패로 끝났으면 그것도 알린다. 지금은 밀 것이 없어도
	// **다음에 또 실패한다** — 원인은 그대로 있다.
	if st, ok := sync.ReadStamp(stampDir()); ok && !st.OK {
		warn = append(warn, fmt.Sprintf("마지막 시도 실패 (%s, %s)",
			st.At.Format("2006-01-02 15:04"), st.Detail))
	}
	if len(warn) == 0 {
		r.add("동기화", OK, fmt.Sprintf("볼트 %d개가 리모트와 같다", used), "")
		return
	}
	sort.Strings(warn)
	r.add("동기화", Warn, strings.Join(warn, " · "),
		"prior sync 를 돌려라 — 다른 머신에서는 이것들이 안 보인다")
}

// stampDir 는 동기화 도장이 사는 자리다. 못 구하면 빈 문자열이고
// ReadStamp 이 조용히 "없다" 로 답한다 — 도장이 없는 것은 고장이 아니다.
func stampDir() string {
	d, err := xdgpath.StateDir()
	if err != nil {
		return ""
	}
	return d
}

// checkLinks 는 **가리키는 대상이 없는 frontmatter 위키링크**를 찾는다.
//
// 링크는 프로젝트를 잇는 유일한 수단인데 아무도 안 보고 있었다 — `internal/core/search`
// 는 Related·Supersedes 를 아예 안 읽고(참조 0건), doctor 는 지금까지 초록불이었다.
// 그래서 개명이나 삭제가 나면 **아무 신호 없이 그래프만 조용히 끊긴다.**
//
// # 등급이 Warn 인 이유
//
// Level 주석(위 §)이 Fail 을 "동작하지 않는다" 로 못박았다. 끊어진 링크는 아무것도
// 못 돌게 하지 않는다 — 회수가 링크를 애초에 안 본다. checkSimilarSlugs 와 같은 급이다.
//
// # 본문을 보지 않는 이유
//
// 실볼트 실측(2026-08-15): 본문 위키링크 291개 중 대상이 없는 것이 15개인데
// **진짜는 1개뿐**이다(오탐률 93%). 나머지는 ```toml 펜스 안의 `[[domain]]`·`[[vault]]`
// (TOML array-of-tables 문법), `[[옛이름]]` 자리표시자, `[[벧전 5:7]]` 성경 인용이다.
// 코드 펜스를 걷어내도 뒤 둘은 살아남는다. checkTeamPortability 주석이 이 프로젝트의
// 죄목으로 든 "소음이 되면 사람은 무시하는 법을 배운다" 에 정면으로 걸린다.
//
// frontmatter 는 우리 방출기가 쓰는 자리라 기준선이 깨끗하다(실볼트 214개 중 dangling 0).
// 그래서 여기서 빨간불이 뜨면 진짜다.
func checkLinks(r *Report, l *store.Layout, notes []store.Note) {
	known, err := l.AllStems()
	if err != nil {
		r.add("링크", Warn, "볼트를 훑을 수 없다: "+err.Error(),
			"볼트 경로와 권한을 확인해라")
		return
	}
	var broken, unwrapped []string
	total := 0
	for _, n := range notes {
		for _, link := range store.LinkTargets(n.Meta) {
			total++
			if !known[link.Target] {
				broken = append(broken, fmt.Sprintf("%s → [[%s]] (%s)", n.Stem, link.Target, link.Field))
				continue
			}
			// **모양이 나쁜 것을 조용히 통과시키지 않는다.** `[[ ]]` 가 없으면
			// 우리는 읽지만 옵시디언은 링크로 안 읽는다 — 백링크 패널에 안 뜨는
			// 반쪽 링크다. 대상이 있으니 broken 은 아니고, 따로 센다.
			if link.Unwrapped {
				unwrapped = append(unwrapped, fmt.Sprintf("%s → %s (%s)", n.Stem, link.Target, link.Field))
			}
		}
	}

	sort.Strings(broken)
	sort.Strings(unwrapped)
	switch {
	case len(broken) > 0:
		detail := strings.Join(broken, " · ")
		if len(unwrapped) > 0 {
			detail += fmt.Sprintf(" · 그리고 [[ ]] 없는 값 %d건", len(unwrapped))
		}
		r.add("링크", Warn, detail,
			"대상이 개명·삭제됐다. related 에서 빼거나 새 이름으로 고쳐라")
	case len(unwrapped) > 0:
		r.add("링크", Warn, strings.Join(unwrapped, " · "),
			"[[ ]] 로 감싸라 — 지금은 옵시디언이 링크로 읽지 않아 백링크가 안 걸린다")
	default:
		r.add("링크", OK, fmt.Sprintf("%d개가 전부 걸린다", total), "")
	}
}

// checkSupersedeSymmetry 는 뒤집기가 **반쪽만 적힌** 자리를 찾는다.
//
// capture.supersede 는 세 가지를 한 번에 한다 — 새 노트의 supersedes, 옛 노트의
// status, 옛 노트의 related 역링크. 그런데 사람이 손으로 status 한 줄만 바꾸거나
// 옵시디언에서 related 를 지우면 그 셋이 갈라지고, **아무도 안 본다.**
//
// 실볼트에 실제로 있다(2026-08-15 실측): `priorcase-결정-유료층-순수E2E-복구불가`
// 와 `priorcase-결정-클라이언트비제공-무료는파일유료는UI` 가 status: superseded 인데
// 아무 노트도 그것을 supersedes 로 가리키지 않는다. 커밋 6259622 에서 status 한 줄과
// 본문 한 문단만 바꾸고 frontmatter 를 안 건드린 결과다. 근본 원인은 `Supersedes`
// 가 한 칸뿐이라 방향전환 하나가 셋을 뒤집을 수 없었던 것이다.
//
// **자동으로 고치지 않는다.** 어느 쪽이 진실인지 기계는 모른다 — 사람이 옵시디언에서
// 지운 것일 수도, 새 노트가 잘못 쓴 것일 수도 있다. "조용히 틀리느니 시끄럽게
// 멈춘다" 는 여기서 "보고하고 Fix 문구로 다음 행동을 준다" 로 읽는다.
func checkSupersedeSymmetry(r *Report, notes []store.Note) {
	byStem := make(map[string]store.Note, len(notes))
	for _, n := range notes {
		byStem[n.Stem] = n
	}
	// 누가 누구를 뒤집는다고 주장하는가.
	supersededBy := map[string]string{}
	for _, n := range notes {
		for _, link := range store.LinkTargets(n.Meta) {
			if link.Kind == store.KindSupersedes {
				supersededBy[link.Target] = n.Stem
			}
		}
	}

	var bad []string
	for target, newer := range supersededBy {
		old, ok := byStem[target]
		if !ok {
			continue // 대상이 아예 없는 것은 checkLinks 가 문다. 두 번 말하지 않는다.
		}
		if old.Meta.Status != "superseded" {
			bad = append(bad, fmt.Sprintf("%s 가 %s 를 뒤집었는데 그쪽 status 가 %q 다",
				newer, target, old.Meta.Status))
		}
		if !hasLink(old.Meta.Related, newer) {
			bad = append(bad, fmt.Sprintf("%s 의 related 에 후속 %s 가 없다", target, newer))
		}
	}
	// 반대 방향: 뒤집혔다고 적혀 있는데 뒤집은 쪽이 없다.
	for _, n := range notes {
		if n.Meta.Status == "superseded" && supersededBy[n.Stem] == "" {
			bad = append(bad, fmt.Sprintf("%s 는 superseded 인데 무엇이 뒤집었는지 아무 데도 없다", n.Stem))
		}
	}

	if len(bad) == 0 {
		r.add("뒤집기", OK, fmt.Sprintf("%d건 전부 양쪽이 맞는다", len(supersededBy)), "")
		return
	}
	sort.Strings(bad)
	r.add("뒤집기", Warn, strings.Join(bad, " · "),
		"prior review <stem> --supersedes <뒤집은-노트> 로 다시 엮어라 (한쪽만 고치면 또 갈라진다)")
}

// hasLink 는 related 목록이 그 stem 을 가리키는지 본다.
func hasLink(related []string, stem string) bool {
	for _, raw := range related {
		for _, link := range store.LinkTargets(store.Meta{Related: []string{raw}}) {
			if link.Target == stem {
				return true
			}
		}
	}
	return false
}

// slugKey 는 유사 비교용 정규화 키다. **capture.slugKey 와 같은 규칙이어야 한다** —
// 한쪽만 고치면 capture 가 거부한 것을 doctor 가 못 보거나 그 반대가 된다.
//
// stem 전체(날짜 포함)를 쓰므로 날짜가 다르면 키가 갈린다. capture 도 그렇게 한다 —
// 며칠 뒤의 같은 주제는 중복이 아니라 후속 결정일 수 있기 때문이다.
func slugKey(stem string) string {
	out := make([]rune, 0, len(stem))
	for _, r := range stem {
		switch r {
		case '-', '_', ' ':
		default:
			if r >= 'A' && r <= 'Z' {
				r += 32
			}
			out = append(out, r)
		}
	}
	return string(out)
}

// checkVaultDir 은 **선언된 볼트를 전부** 본다.
//
// 하나만 보면 나머지가 깨져 있어도 초록불이 뜬다 — 그 볼트를 쓰는 프로젝트에서
// 일할 때에야 드러나고, 그때는 기록이 이미 실패한 뒤다.
func checkVaultDir(r *Report, c *config.Config) {
	for _, v := range c.Vaults {
		label := "볼트"
		if len(c.Vaults) > 1 {
			label = "볼트 " + v.Name
		}
		fi, err := os.Stat(v.Path)
		switch {
		case err != nil:
			r.add(label, Fail, fmt.Sprintf("%s 에 접근할 수 없다 (%v)", v.Path, err),
				"설정의 vault 경로를 확인하라")
		case !fi.IsDir():
			r.add(label, Fail, v.Path+" 가 디렉토리가 아니다", "설정의 vault 경로를 확인하라")
		default:
			r.add(label, OK, v.Path, "")
		}
	}
}

// checkDomainFolders 는 선언된 도메인 중 폴더가 아직 없는 것을 센다.
//
// **Fail 이 아니다.** 폴더는 그 도메인의 첫 결정을 쓸 때 만들어지므로, 결정이 아직
// 없는 프로젝트는 폴더가 없는 게 정상이다. 그래도 세어서 보여 주는 이유는 오타를
// 잡기 위해서다 — folder 이름을 잘못 적으면 영원히 "아직 없음" 으로 남는다.
func checkDomainFolders(r *Report, c *config.Config, l *store.Layout) {
	// **도메인이 0개면 아무것도 기록할 수 없다.** 그런데 겉으로는 조용하다 —
	// 훅은 돌고 안전망은 표시까지 하는데 승격 단계에서 막힌다. 새 사용자가 정확히
	// 이 상태로 시작하므로 가장 크게 알려야 한다.
	if len(c.Domain) == 0 {
		r.add("도메인", Fail, "설정에 [[domain]] 이 하나도 없다 — 아무것도 기록되지 않는다",
			"프로젝트마다 [[domain]] 블록을 추가하거나, 최소한 default_domain 을 적어라")
		return
	}
	if c.DefaultDomain == "" {
		r.add("도메인", Warn,
			fmt.Sprintf("%d개 · default_domain 이 없다 — 어느 paths 에도 안 걸리는 곳에서는 기록되지 않는다",
				len(c.Domain)),
			`default_domain = "common" 처럼 폴백을 적어라`)
	} else if !hasPrefix(c, c.DefaultDomain) {
		r.add("도메인", Fail,
			fmt.Sprintf("default_domain = %q 인데 그런 [[domain]] 이 없다", c.DefaultDomain),
			"오타이거나 블록이 빠졌다")
		return
	} else {
		r.add("도메인", OK,
			fmt.Sprintf("%d개 · 폴백 %s", len(c.Domain), c.DefaultDomain), "")
	}

	checkTeamPortability(r, c, l)

	dirs := l.DecisionDirs()
	var missing []string
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			missing = append(missing, l.RelPath(d))
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		r.add("도메인 폴더", OK, fmt.Sprintf("%d개 전부 있다", len(dirs)), "")
		return
	}
	r.add("도메인 폴더", OK,
		fmt.Sprintf("%d개 중 %d개는 아직 없다 %v — 첫 결정을 쓸 때 만들어진다",
			len(dirs), len(missing), missing),
		"이름을 잘못 적은 게 아닌지 설정의 folder 값을 확인하라")
}

// checkUndeclared 가 이 패키지의 핵심이다.
//
// 볼트에 결정 폴더가 있는데 설정에 없으면 그 프로젝트의 결정이 **전부** 색인과
// 회수에서 빠진다. 그런데 색인은 정상 생성되고 회수도 에러를 안 낸다 — 없는 것처럼 군다.
func checkUndeclared(r *Report, l *store.Layout) {
	dirs, err := l.UndeclaredDecisionDirs()
	if err != nil {
		r.add("미선언 도메인", Warn, "확인할 수 없다: "+err.Error(), "")
		return
	}
	if len(dirs) == 0 {
		r.add("미선언 도메인", OK, "없다", "")
		return
	}
	var rel []string
	for _, d := range dirs {
		rel = append(rel, l.RelPath(d))
	}
	r.add("미선언 도메인", Fail,
		fmt.Sprintf("%v — 이 폴더의 결정은 색인·회수에서 통째로 빠진다", rel),
		"설정에 [[domain]] 블록을 추가하고 prior index 를 다시 돌려라")
}

func checkNotes(r *Report, l *store.Layout) []store.Note {
	notes, skipped, err := l.List()
	if err != nil {
		r.add("결정 노트", Fail, "읽을 수 없다: "+err.Error(), "볼트 경로와 권한을 확인하라")
		return nil
	}
	if len(skipped) > 0 {
		// **갈래마다 사람이 할 일이 정반대다.**
		//
		// 예전에는 둘을 묶어 "frontmatter 를 정본 10키로 고쳐라" 하나만 냈다.
		// 그 지시가 사고를 만들었다 — 2026-08-21 에 다른 머신의 더 새 판이 쓴 노트를
		// 사람이 그 말대로 옛 모양으로 되돌려 다중값 supersedes 를 강등시켰다.
		// 판 갈림은 **고칠 것이 노트가 아니라 이쪽 바이너리**다.
		var newer, broken []string
		for _, s := range skipped {
			if s.LooksNewer() {
				newer = append(newer, l.RelPath(s.Path))
				continue
			}
			broken = append(broken, l.RelPath(s.Path))
		}
		sort.Strings(newer)
		sort.Strings(broken)

		var detail, fix []string
		if len(newer) > 0 {
			detail = append(detail,
				fmt.Sprintf("더 새 판이 쓴 모양이라 못 읽는다 %v", newer))
			fix = append(fix,
				"prior 를 올려라 — 그 노트를 **손으로 고치지 마라.** "+
					"옛 모양으로 되돌리면 다른 머신이 쌓은 것을 지운다")
		}
		if len(broken) > 0 {
			detail = append(detail,
				fmt.Sprintf("frontmatter 가 깨져 못 읽는다 %v", broken))
			fix = append(fix, "frontmatter 를 정본 10키로 고쳐라. prior index 가 이유를 알려준다")
		}
		r.add("결정 노트", Fail,
			fmt.Sprintf("%d건 중 %d건을 읽지 못했다 — %s",
				len(notes)+len(skipped), len(skipped), strings.Join(detail, " · ")),
			strings.Join(fix, " / "))
		return notes
	}
	r.add("결정 노트", OK, fmt.Sprintf("%d건 전부 읽힌다", len(notes)), "")
	return notes
}

func hasPrefix(c *config.Config, prefix string) bool {
	for _, d := range c.Domain {
		if d.Prefix == prefix {
			return true
		}
	}
	return false
}

// checkTeamPortability 는 이 설정이 **다른 사람 기계에서도 통하는지** 본다.
//
// `paths` 는 절대 경로다. 볼트를 팀과 공유하면 팀원의 체크아웃 자리가 다르므로
// 그 사람에게는 **하나도 안 걸린다.** 그러면 기록이 폴백 도메인으로 새거나 아예
// 막히는데, **겉으로는 조용하다** — 훅은 돌고 안전망은 표시까지 하고 승격에서 멎는다.
// 새 팀원이 정확히 그 상태로 시작한다.
//
// **볼트가 git 저장소일 때만 말한다.** 혼자 쓰는 볼트에서는 paths 만으로 충분하고,
// 그 사람에게 이 경고는 고칠 이유가 없는 소음이다. 이 프로젝트는 소음을 죄목으로
// 삼는다 — 안전망이 소음이 되면 사람은 무시하는 법을 배운다. 볼트가 git 아래 있다는
// 것은 **실제로 공유되고 있다는 신호**이고, 그때만 이 경고가 행동을 부른다.
func checkTeamPortability(r *Report, c *config.Config, l *store.Layout) {
	if config.RepoFor(l.Vault()) == "" && !isGitDir(l.Vault()) {
		return
	}
	var pathOnly []string
	suggest := map[string]string{}
	withRepo := 0
	for _, d := range c.Domain {
		switch {
		case len(d.Repos) > 0:
			withRepo++
		case len(d.Paths) > 0:
			// 폴백 도메인은 원래 경로가 없어도 되는 자리라 세지 않는다.
			if d.Prefix == c.DefaultDomain {
				continue
			}
			repo, known := repoHint(d.Paths)
			// **고칠 수 없는 것은 말하지 않는다.** 경로가 이 기계에 다 있는데
			// 어느 것도 origin 이 없으면 repos 를 적을 방법이 없다 — 그런데도
			// "repos 를 더해라" 고 하면 그 경고는 영영 떠 있고, 늘 뜨는 경고는
			// 무시하는 법을 가르친다. 그러면 고칠 수 있는 도메인이 하나 늘어도
			// 같이 묻힌다.
			//
			// **모르는 것을 침묵으로 바꾸지는 않는다.** 경로가 없으면(팀원의
			// 체크아웃 자리) 판정할 수 없으므로 경고를 유지한다.
			if known && repo == "" {
				continue
			}
			pathOnly = append(pathOnly, d.Prefix)
			if repo != "" {
				suggest[d.Prefix] = repo
			}
		}
	}
	if len(pathOnly) == 0 {
		if withRepo > 0 {
			r.add("팀 이식성", OK,
				fmt.Sprintf("%d개 도메인이 repos 로 잡힌다 — 팀원이 어디에 체크아웃해도 같다", withRepo), "")
		}
		return
	}
	sort.Strings(pathOnly)
	// **무엇을 적을지까지 준다.** "repos 를 더해라" 만으로는 사람이 git remote 를
	// 치고 형식을 맞춰야 한다. 우리가 이미 아는 값이면 그대로 준다 — 진단은
	// 다음 행동을 짧게 만드는 것이 일이다.
	fix := `각 [[domain]] 에 repos = ["owner/repo"] 를 더해라 (paths 는 그대로 둬도 된다)`
	if len(suggest) > 0 {
		var parts []string
		for _, p := range pathOnly {
			if rp := suggest[p]; rp != "" {
				parts = append(parts, fmt.Sprintf("%s → repos = [%q]", p, rp))
			}
		}
		if len(parts) > 0 {
			fix = strings.Join(parts, " · ") + " (paths 는 그대로 둬도 된다)"
		}
	}
	r.add("팀 이식성", Warn,
		fmt.Sprintf("%d개 도메인이 paths 로만 잡힌다 %v — 볼트를 팀과 공유하면 그 사람들에게는 안 걸린다",
			len(pathOnly), pathOnly),
		fix)
}

// repoHint 는 이 경로들에서 알아낼 수 있는 저장소 이름과, **판정할 수 있었는지**를 준다.
//
// known 이 false 면 경로가 이 기계에 없어서 모르는 것이다 — 그건 침묵의 근거가
// 못 된다. known 이 true 이고 repo 가 비면 **repos 를 적을 방법이 없다**는 뜻이다.
func repoHint(paths []string) (repo string, known bool) {
	known = true
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			known = false
			continue
		}
		if r := config.RepoFor(p); r != "" {
			return r, true
		}
	}
	return "", known
}

// isGitDir 는 볼트 자체가 git 저장소인지 본다 (remote 가 없어도 된다).
//
// config.RepoFor 는 origin 이 있어야 값을 준다. 팀이 아직 remote 를 안 걸었거나
// 사내 호스트를 다른 이름으로 걸어 뒀을 수 있어서, "git 아래 있다" 는 사실만
// 따로 본다. 위로 거슬러 올라가지 않는다 — 볼트가 남의 저장소 **안에** 우연히
// 들어 있는 경우까지 공유로 볼 수는 없다.
func isGitDir(dir string) bool {
	_, err := os.Stat(dir + string(os.PathSeparator) + ".git")
	return err == nil
}

// gitIgnores 는 볼트의 무시 목록에 rel 이 있는지 본다.
//
// `.gitignore` 와 `.git/info/exclude` 만 본다. 전역 무시 파일(core.excludesFile)이나
// 상위 디렉토리의 규칙은 안 본다 — 그 경우 오탐(있는데 없다고 말함)이 나지만,
// 반대(없는데 있다고 말함)보다 낫다. 없는데 있다고 하면 충돌을 그대로 두게 된다.
func gitIgnores(vault, rel string) bool {
	for _, p := range []string{
		filepath.Join(vault, ".gitignore"),
		filepath.Join(vault, ".git", "info", "exclude"),
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.TrimPrefix(line, "/") == rel {
				return true
			}
		}
	}
	return false
}

// checkStaleKeys 는 **없앤 색인이 남긴 잔재**를 본다 — 설정 키와 파일 둘 다.
//
// # 설정 키
//
// `[naming] index` 는 무시되지만 필드를 지울 수는 없다 — `config.Load` 가
// `DisallowUnknownFields` 를 쓰므로 지우면 그 키가 적힌 설정이 통째로 로드 실패하고,
// 설정은 머신 사이를 건너오지 않아서 어느 머신에는 옛 키가 남아 있는 것이 정상이다
// (Naming.Index 주석). 조용히 무시하면 설정을 읽는 사람이 색인이 유지되는 줄로 안다.
//
// # 남아 있는 파일
//
// **이쪽이 더 아프다.** 2026-08-25 에 실제로 겪었다: 집 머신이 아직 제거 이전 판이라
// `prior capture` 마다 색인을 다시 만들었고, 없애는 커밋에서 `.gitignore` 항목까지
// 같이 뺀 탓에 그것이 git 을 타고 다른 머신으로 건너왔다. 사용자가 "없앴는데 다시
// 생겼다" 로 발견했다 — 도구는 아무 말도 하지 않았다.
//
// 볼트를 지우지는 않는다(이 프로젝트의 규칙이다). 있다는 사실과 왜 남았는지만 말한다.
func checkStaleKeys(r *Report, c *config.Config, l *store.Layout) {
	var lines []string
	fix := ""
	if c != nil && strings.TrimSpace(c.Naming.Index) != "" {
		lines = append(lines, "설정의 [naming] index")
		fix = "설정에서 그 한 줄을 지워라"
	}
	if p := staleIndexFile(c, l); p != "" {
		lines = append(lines, l.RelPath(p))
		if fix != "" {
			fix += " · "
		}
		fix += "파일은 지워도 된다 — 다시 생기면 **다른 머신이 아직 옛 판이다** " +
			"(prior doctor 의 `판` 검사가 어느 머신인지 말한다)"
	}
	if len(lines) == 0 {
		return // 할 말이 없으면 줄을 만들지 않는다
	}
	r.add("낡은 색인", OK,
		strings.Join(lines, " · ")+" — 결정 색인은 2026-08-24 에 없앴다", fix)
}

// staleIndexFile 은 없앤 색인이 볼트에 남아 있으면 그 경로를 준다.
//
// 설정에 옛 키가 있으면 그 경로를 먼저 보고, 없으면 알려진 이름 둘을 본다 —
// 키를 이미 지운 머신에도 파일은 남아 있을 수 있다(그게 2026-08-25 의 상황이다).
func staleIndexFile(c *config.Config, l *store.Layout) string {
	if l == nil {
		return ""
	}
	var cands []string
	if c != nil && strings.TrimSpace(c.Naming.Index) != "" {
		cands = append(cands, c.Naming.Index)
	}
	cands = append(cands, "_meta/00-결정-색인.md", "_meta/00-decision-index.md")
	for _, rel := range cands {
		p := filepath.Join(l.Vault(), filepath.FromSlash(rel))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
