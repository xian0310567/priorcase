// Package worklog 는 **결정이 되기 전의 것들**을 쌓는다.
//
// [[priorcase-decision-기록계층-2단-worklog신설-판정시점-이원화-2026-08-19]] 이 만든
// 두 번째 등급이다. 그 전까지 이 저장소에는 등급이 하나뿐이었다 — 결정 노트. 그래서
// "확정되지 않은 것" 은 갈 곳이 없었고, 판별기는 그걸 **버리는 것으로** 처리했다.
// 원장 23건 중 11건이 "아직 최종 결정이 내려지지 않았다" 로 기각됐다.
//
// 버린 것이 사소해서가 아니다. 같은 세션에서 사람이 손으로 결정 노트 8건을 썼다 —
// 판별기가 버린 바로 그 내용을. 문제는 판정이 아니라 **담을 그릇이 없었다는 것**이다.
//
// 그래서 여기가 그 그릇이다. 검토한 대안, 각각을 왜 기각했는지, 측정 결과, 아직
// 미결인 것과 그것이 확정되는 조건 — 결정 노트로 올리기엔 이르지만 잃으면 안 되는 것들.
//
// # 결정 노트와 무엇이 다른가
//
// **회수에 자동 주입되지 않는다.** 이것이 등급을 나눈 이유의 전부다.
// 회수는 Limit 3 · MinScore 1 의 고정 슬롯이라(core/search), 사소한 것과 중요한 것이
// 같은 가중치로 경쟁하면 볼트가 커질수록 회수가 나빠진다. 작업 로그는 물어볼 때만
// 검색된다 — 쌓는 비용이 회수 품질을 갉아먹지 않는다.
//
// # 형식은 rollup 이 정한다
//
// 날짜 헤딩(`## YYYY-MM-DD`)이 경계이고 항목은 그 아래 `###` 로 들어간다.
// core/rollup 의 weekBlocks 가 그 헤딩으로 주를 쪼개므로, 이 형식을 벗어나면
// `prior rollup` 이 그 주를 통째로 못 본다.
package worklog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Entry 는 작업 로그 항목 하나다.
type Entry struct {
	Domain string
	Date   string // YYYY-MM-DD. 비면 오늘
	Time   string // HH:MM. 비면 지금
	// Title 은 한 줄이다. `###` 헤딩이 되므로 줄바꿈이 들어가면 안 된다.
	Title string
	// Body 는 마크다운이다. 절 제목은 `####` 이하를 쓴다 — `##` 을 쓰면 rollup 이
	// 그 자리에서 주 블록을 끊는다(core/rollup 의 anyH2).
	Body string
	// Session 은 이 항목이 나온 대화의 세션 id 다.
	Session string
	// Source 는 이 항목을 만든 구간의 id 다. **중복 기록을 막는 열쇠다** —
	// 데몬은 같은 구간을 여러 번 훑을 수 있고, 그때마다 항목이 쌓이면 로그가
	// 같은 말을 반복한다. 비어 있으면 중복 검사를 건너뛴다(사람이 직접 쓴 경우).
	Source string
	Tags   []string
}

// Result 는 Append 의 결과다.
type Result struct {
	Path string
	// Skipped 는 같은 Source 가 이미 있어 쓰지 않았다는 뜻이다. 에러가 아니다 —
	// 데몬이 같은 구간을 다시 훑는 것은 정상 동작이다.
	Skipped bool
}

// Append 는 항목을 도메인의 작업 로그에 덧붙인다.
//
// 오늘 날짜 헤딩이 **파일 끝에 이미 있으면** 그 아래에 붙이고, 없으면 새로 연다.
// 헤딩을 매번 새로 열어도 rollup 은 같은 주로 묶지만(weekBlocks 가 누적한다),
// 옵시디언에서 하루가 여러 덩어리로 갈라져 읽기 나쁘다.
func Append(l *store.Layout, e Entry) (Result, error) {
	if strings.TrimSpace(e.Title) == "" {
		return Result{}, fmt.Errorf("작업 로그 항목에 제목이 없다")
	}
	if strings.ContainsAny(e.Title, "\r\n") {
		return Result{}, fmt.Errorf("작업 로그 제목에 줄바꿈이 들어 있다: %q", e.Title)
	}
	now := time.Now()
	if e.Date == "" {
		e.Date = now.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", e.Date); err != nil {
		return Result{}, fmt.Errorf("작업 로그 날짜가 YYYY-MM-DD 가 아니다: %q", e.Date)
	}
	if e.Time == "" {
		e.Time = now.Format("15:04")
	}

	// 볼트는 도메인이 정한다 — capture.Do 와 같은 규칙이다. 호출부에 맡기면
	// 훅·CLI·MCP 가 서로 다른 볼트에 쓴다.
	l, err := l.For(e.Domain)
	if err != nil {
		return Result{}, err
	}
	path, err := l.WorklogPath(e.Domain)
	if err != nil {
		return Result{}, err
	}

	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		existing = []byte(header(l, e.Domain))
	case err != nil:
		return Result{}, fmt.Errorf("작업 로그를 읽을 수 없다 (%s): %w", l.RelPath(path), err)
	}

	cur := string(existing)
	if e.Source != "" && strings.Contains(cur, sourceMark(e.Source)) {
		return Result{Path: path, Skipped: true}, nil
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(cur, "\n"))
	b.WriteString("\n")
	if !hasTrailingDate(cur, e.Date) {
		fmt.Fprintf(&b, "\n## %s\n", e.Date)
	}
	b.WriteString(render(e))

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := store.WriteFileAtomic(path, []byte(b.String()), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Path: path}, nil
}

// render 는 항목 하나를 마크다운으로 만든다.
func render(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s · %s\n", e.Time, e.Title)
	if body := strings.TrimSpace(demoteHeadings(e.Body)); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	// 꼬리표는 한 줄이다. 세션·구간·태그가 여기 모인다 — 본문에 섞으면 사람이
	// 읽을 때 걸리고, 안 남기면 나중에 어느 대화에서 나왔는지 복원할 수 없다.
	var tail []string
	if e.Session != "" {
		tail = append(tail, "session "+e.Session)
	}
	if e.Source != "" {
		tail = append(tail, sourceMark(e.Source))
	}
	for _, t := range e.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tail = append(tail, "#"+strings.ReplaceAll(t, " ", "-"))
		}
	}
	if len(tail) > 0 {
		fmt.Fprintf(&b, "\n<sub>%s</sub>\n", strings.Join(tail, " · "))
	}
	return b.String()
}

// demoteHeadings 는 본문의 h1~h3 을 h4 아래로 내린다.
//
// **rollup 이 `## ` 에서 주 블록을 끊기 때문이다**(core/rollup 의 anyH2). 항목 제목이
// `###` 이므로 본문에 `##` 가 하나라도 있으면 그 항목의 나머지가 주 블록 밖으로
// 떨어져 나가고, `prior rollup` 이 그것을 영영 못 본다.
//
// **호출부에 맡길 수 없다.** 본문을 쓰는 쪽은 판별기(LLM)이고 우리는 그 출력을
// 통제하지 못한다. 실측으로 물렸다: 스키마 설명과 판별기 지시문 양쪽에 "절 제목은
// #### 이하" 를 적어 뒀는데도, 실제 판정이 `## 검토한 대안` 을 냈다. 지시가 아니라
// **쓰는 자리에서** 막는다.
//
// 코드 펜스 안은 건드리지 않는다 — 셸 주석(`# 뭐뭐`)이 헤딩으로 오해되면 코드가 깨진다.
func demoteHeadings(body string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		n := 0
		for n < len(line) && line[n] == '#' {
			n++
		}
		// `#피드백` 처럼 해시태그는 헤딩이 아니다. 마크다운도 공백을 요구한다.
		if n >= len(line) || line[n] != ' ' {
			continue
		}
		if n < 4 {
			lines[i] = strings.Repeat("#", 4-n) + line
		}
	}
	return strings.Join(lines, "\n")
}

// sourceMark 는 구간 id 를 파일에 남기는 표식이다. 중복 검사가 이 문자열을 찾는다.
//
// 백틱으로 감싸는 이유: 구간 id 는 경로+오프셋이라 옵시디언이 링크나 강조로
// 오해할 문자를 담는다. 코드 스팬 안에서는 아무것도 해석되지 않는다.
func sourceMark(id string) string { return "`@" + id + "`" }

// hasTrailingDate 는 파일의 **마지막** 날짜 헤딩이 date 인지 본다.
//
// 마지막만 보는 이유: 중간에 있는 같은 날짜 밑에 끼워 넣으면 그 뒤의 항목들과
// 시간 순서가 뒤집힌다. 덧붙이기는 언제나 파일 끝이다.
func hasTrailingDate(body, date string) bool {
	last := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
	}
	return last == date
}

// Scope 는 cwd 와 cross-project 플래그에서 Search 에 넘길 도메인을 정한다.
//
// **어댑터마다 이 계산을 되풀이하지 않게 여기 둔다.** cli 와 mcp 가 각자 하면
// 한쪽만 고쳐진 채로 남고, 그 어긋남은 "이 프로젝트만" 이라고 말한 사용자에게
// 남의 프로젝트 메모가 나온 뒤에야 드러난다 — 실제로 그렇게 샜다.
func Scope(c *config.Config, cwd string, crossProject bool) string {
	if crossProject || cwd == "" {
		return ""
	}
	return c.DomainForCwd(cwd)
}

// Hit 은 작업 로그 검색 결과 하나다.
type Hit struct {
	Domain string
	Date   string
	Time   string
	Title  string
	Body   string
	Path   string
	Score  int
}

// entryHeading 은 항목 헤딩이다 (`### HH:MM · 제목`). 시각은 없을 수도 있다 —
// 사람이 손으로 쓴 항목은 `### 제목` 만 있는 것이 자연스럽다.
var entryHeading = regexp.MustCompile(`(?m)^### (?:(\d{2}:\d{2}) · )?(.*)$`)

// dateHeading 은 날짜 헤딩이다. core/rollup 과 같은 규약이다.
var dateHeading = regexp.MustCompile(`(?m)^## (\d{4}-\d{2}-\d{2})`)

// Search 는 작업 로그에서 질의에 걸리는 항목을 찾는다.
//
// **회수(core/search)와 달리 자동 주입되지 않는다.** 이것이 등급을 나눈 이유다 —
// 여기 쌓이는 양은 결정 노트보다 훨씬 많아서, 매 프롬프트에 섞이면 결정 노트가
// 밀려난다. 물어볼 때만 온다.
//
// scope 가 비면 선언된 모든 도메인을 본다. 비어 있지 않으면 **그 도메인만** 본다.
//
// # scope 를 받는 이유
//
// 처음에는 안 받았고, 그래서 `--cross-project=false` 가 **반쪽만 지켜졌다.**
// 결정 노트는 core/search 의 filterByDomain 이 걸렀는데 작업 로그는 안 걸려서,
// alpha 에서 좁혀 물었는데 beta 의 진행 중 메모가 딸려 나왔다 — 실제로 재현했다.
// 사용자가 "이 프로젝트만" 이라고 말한 자리에서 남의 프로젝트 것이 나오는 것은
// 회수 품질 문제가 아니라 약속을 어긴 것이다.
//
// # core/search 와 달리 넓히지 않는다
//
// core/search 는 좁힌 결과가 비면 전체로 넓힌다(옛 셸 동작). 여기서는 안 넓힌다.
// 그쪽은 "관련 결정이 하나도 없는 것보다는 남의 것이라도 보여 주는 게 낫다" 는
// 판단이고, 결정 노트는 확정된 것이라 남의 도메인 것도 읽을 값어치가 있다.
// 작업 로그는 확정 전의 것이라 남의 프로젝트에서 진행 중인 검토가 이 대화에
// 끼어들 이유가 없다.
//
// 점수는 제목 히트에 3, 본문 히트에 1 이다. core/search 의 weightHead·weightBody 와
// 같은 비율이라 두 계층의 결과를 나란히 놓고 읽을 수 있다. 다만 결정 노트와 달리
// **제목 히트가 0 이어도 버리지 않는다** — 작업 로그의 제목은 한 줄이고 근거는
// 본문에 있으므로, 본문만 걸리는 것이 정상이다.
func Search(l *store.Layout, keywords []string, scope string, limit int) ([]Hit, error) {
	if len(keywords) == 0 {
		return nil, nil
	}
	var hits []Hit
	for _, prefix := range l.Prefixes() {
		if scope != "" && prefix != scope {
			continue
		}
		dl, err := l.For(prefix)
		if err != nil {
			continue
		}
		path, err := dl.WorklogPath(prefix)
		if err != nil {
			continue
		}
		body, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("작업 로그를 읽을 수 없다 (%s): %w", dl.RelPath(path), err)
		}
		for _, e := range parse(string(body)) {
			title, text := strings.ToLower(e.Title), strings.ToLower(e.Body)
			score := 0
			for _, k := range keywords {
				if search.Matches(title, k) {
					score += 3
				}
				if search.Matches(text, k) {
					score++
				}
			}
			if score == 0 {
				continue
			}
			e.Domain, e.Path, e.Score = prefix, path, score
			hits = append(hits, e)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		// 동점이면 최신이 앞이다. 작업 로그는 진행 중인 것을 담으므로 오래된
		// 검토보다 어제의 검토가 거의 언제나 더 쓸모 있다.
		if hits[i].Date != hits[j].Date {
			return hits[i].Date > hits[j].Date
		}
		return hits[i].Time > hits[j].Time
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// SessionTitles 는 그 세션에서 이 도메인의 작업 로그에 쌓인 항목 제목들이다.
//
// **세션 끝 판정이 이걸 근거로 받는다.** 도중 판정이 대안·기각 이유·측정을 미리
// 쌓아 두므로, 아크 전체를 볼 때 발췌만이 아니라 그 축적분을 함께 넘길 수 있다 —
// 발췌 상한(daemon.maxExcerpt)에 잘려 나간 앞부분이 여기 남아 있다.
//
// 제목만 준다. 본문까지 넘기면 판별기 입력이 발췌보다 커져서 주객이 뒤집힌다.
func SessionTitles(l *store.Layout, domain, session string, limit int) []string {
	if session == "" {
		return nil
	}
	dl, err := l.For(domain)
	if err != nil {
		return nil
	}
	path, err := dl.WorklogPath(domain)
	if err != nil {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil // 없는 것이 정상이다 — 이 세션의 첫 판정일 수 있다
	}
	mark := "session " + session
	var out []string
	for _, e := range parse(string(body)) {
		if strings.Contains(e.Body, mark) {
			out = append(out, e.Title)
		}
	}
	// 넘치면 **최근 것**을 남긴다. parse 는 파일 순서(=시간 순서)로 주므로 뒤가 최근이다.
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// parse 는 작업 로그 본문을 항목으로 쪼갠다.
func parse(body string) []Hit {
	dates := dateHeading.FindAllStringSubmatchIndex(body, -1)
	if len(dates) == 0 {
		return nil
	}
	var out []Hit
	for i, d := range dates {
		end := len(body)
		if i+1 < len(dates) {
			end = dates[i+1][0]
		}
		date := body[d[2]:d[3]]
		block := body[d[1]:end]

		ents := entryHeading.FindAllStringSubmatchIndex(block, -1)
		for j, m := range ents {
			bodyEnd := len(block)
			if j+1 < len(ents) {
				bodyEnd = ents[j+1][0]
			}
			h := Hit{Date: date, Title: strings.TrimSpace(group(block, m, 2))}
			h.Time = group(block, m, 1)
			h.Body = strings.TrimSpace(block[m[1]:bodyEnd])
			out = append(out, h)
		}
	}
	return out
}

// group 은 하위 그룹 n 의 문자열이다. 매칭 안 된 그룹은 빈 문자열.
func group(s string, m []int, n int) string {
	if 2*n+1 >= len(m) || m[2*n] < 0 {
		return ""
	}
	return s[m[2*n]:m[2*n+1]]
}

func header(l *store.Layout, prefix string) string {
	return fmt.Sprintf(`---
title: %s %s
type: worklog
tags: [worklog, %s]
---

# %s — %s

> %s
`,
		prefix, l.Lang().T("작업 로그", "worklog"),
		prefix,
		prefix, l.Lang().T("작업 로그", "worklog"),
		l.Lang().T(
			"확정 전의 것들이 여기 쌓인다 — 검토한 대안, 기각 이유, 측정 결과, 미결 사항. "+
				"확정된 결정은 결정 노트로 올라가고 회수가 자동으로 주입한다. **이 파일은 물어볼 때만 검색된다.**",
			"Everything before it settles lands here — alternatives weighed, what was ruled out and why, "+
				"measurements, open questions. Settled decisions graduate to decision notes, which recall injects "+
				"automatically. **This file is searched only when you ask for it.**"))
}
