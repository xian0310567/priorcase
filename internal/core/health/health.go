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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/index"
	"github.com/xian0310567/priorcase/internal/core/schema"
	"github.com/xian0310567/priorcase/internal/core/store"
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
	checkDomainFolders(r, c, l)
	checkUndeclared(r, l)
	notes := checkNotes(r, l)
	checkSchema(r, l, notes)
	checkSimilarSlugs(r, notes)
	checkIndex(r, l, notes)
	checkIndexInGit(r, l)
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
		var rel []string
		for _, s := range skipped {
			rel = append(rel, l.RelPath(s.Path))
		}
		r.add("결정 노트", Fail,
			fmt.Sprintf("%d건 중 %d건을 읽지 못했다 %v", len(notes)+len(skipped), len(skipped), rel),
			"frontmatter 를 정본 10키로 고쳐라. prior index 가 이유를 알려준다")
		return notes
	}
	r.add("결정 노트", OK, fmt.Sprintf("%d건 전부 읽힌다", len(notes)), "")
	return notes
}

// checkIndex 는 디스크의 색인이 지금 볼트와 맞는지 본다.
//
// **색인이 결정적이라서 가능한 검사다** — 같은 볼트면 같은 바이트가 나오므로,
// 다시 만들어 보고 비교하면 낡았는지 알 수 있다. 생성 시각을 안 넣기로 한 결정이
// 여기서 값을 한다.
func checkIndex(r *Report, l *store.Layout, notes []store.Note) {
	want, _, err := index.Build(l)
	if err != nil {
		r.add("색인", Fail, "생성할 수 없다: "+err.Error(), "")
		return
	}
	got, err := os.ReadFile(l.IndexPath())
	if os.IsNotExist(err) {
		r.add("색인", Fail, l.RelPath(l.IndexPath())+" 가 없다", "prior index")
		return
	}
	if err != nil {
		r.add("색인", Fail, "읽을 수 없다: "+err.Error(), "")
		return
	}
	if !bytes.Equal(want, got) {
		r.add("색인", Warn, "볼트와 어긋난다 — 노트를 손으로 고쳤거나 색인을 안 돌렸다", "prior index")
		return
	}
	r.add("색인", OK, fmt.Sprintf("%d건과 일치한다", len(notes)), "")
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

// checkIndexInGit 은 **파생물이 버전 관리에 들어가 있는지** 본다.
//
// 색인은 결정 노트에서 언제든 다시 만들 수 있다. 그런데 `prior capture` 가 매번
// 통째로 다시 쓰므로, 볼트를 git 으로 공유하면 **두 사람이 각자 하나씩 기록할
// 때마다 충돌한다.** 실측으로 재현했다 — 결정 노트는 파일이 달라 깨끗이 병합되고
// 색인만 충돌한다.
//
// 그 충돌은 고약하다. 내용이 겹치는 것이 아니라 각자 옳은 표를 만든 것뿐인데,
// 사람은 그걸 손으로 풀어야 하고 잘못 풀면 남의 결정이 색인에서 사라진다 —
// 그러면 회수가 그 결정을 못 본다.
//
// **볼트가 git 이고 색인이 무시 목록에 없을 때만 말한다.** 혼자 쓰거나 git 이
// 아니면 아무 문제가 아니다.
func checkIndexInGit(r *Report, l *store.Layout) {
	if !isGitDir(l.Vault()) {
		return
	}
	rel := l.RelPath(l.IndexPath())
	if gitIgnores(l.Vault(), rel) {
		r.add("색인/git", OK, rel+" 은 무시 목록에 있다 — 병합 충돌이 없다", "")
		return
	}
	r.add("색인/git", Warn,
		rel+" 이 git 에 들어 있다 — 두 사람이 각자 기록할 때마다 충돌한다",
		"색인은 노트에서 다시 만들 수 있는 파생물이다. 볼트에서:\n"+
			"       echo '"+rel+"' >> .gitignore && git rm --cached '"+rel+"'")
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
