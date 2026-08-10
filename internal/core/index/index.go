// Package index 는 전 프로젝트 결정을 한 표로 만든다.
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// header 는 색인 표의 머리줄이다. 색인은 사람만 보는 진단 출력이 아니라 볼트에
// 남아서 에이전트가 읽는 산출물이므로, 볼트 언어를 따른다. 구분선은 markdown
// 문법이라 언어를 타지 않는다.
func header(lang i18n.Lang) string {
	return lang.T(
		"| 날짜 | domain | summary | status | outcome | 링크 |\n",
		"| Date | domain | summary | status | outcome | Link |\n") +
		"| --- | --- | --- | --- | --- | --- |\n"
}

// escapeCell 은 표 셀 안에서 파이프가 열을 쪼개지 않게 한다.
func escapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// Result 는 색인 생성의 결과다.
//
// Rows 만 돌려주던 시절에는 "47행 생성" 이 완전한 색인인지 6건을 흘린 색인인지
// 구별할 수단이 호출자에게 없었다. Skipped 가 그 자리를 채운다 — 비어 있지
// 않으면 색인은 불완전하고, 호출자는 그 사실을 사용자에게 반드시 알려야 한다.
type Result struct {
	// Rows 는 색인 표에 실제로 들어간 행(노트) 수다.
	Rows int
	// Skipped 는 읽지 못해 색인에서 빠진 결정 노트다. 경로 오름차순.
	Skipped []store.SkippedNote
	// Preserved 는 색인 자리에 있던 **남의 파일**을 대피시킨 경로다. 비었으면 없었다.
	//
	// 호출자는 이것이 비어 있지 않으면 **반드시 사용자에게 알려야 한다.** 조용히
	// 백업만 남기면 사용자는 자기 문서가 어디 갔는지 모른 채 사라졌다고 여긴다.
	Preserved string
}

// Build 는 색인 문서 전체와 그 결과(행 수·건너뛴 노트)를 만든다.
// 최신 날짜가 위로 온다.
//
// 행 수를 방출된 markdown 에서 세지 않고 별도로 돌려주는 이유: 문자열을 다시
// 스캔해서(예: "\n| 2" 부분 문자열 개수) 세면 연대가 "2"로 시작하는(2000~2999년)
// 우연에 기대게 된다. 여기서는 이미 손에 쥔 notes 슬라이스의 길이를 그대로
// 돌려주므로 그런 가정이 아예 필요 없다.
func Build(l *store.Layout) ([]byte, Result, error) {
	notes, skipped, err := l.List()
	if err != nil {
		return nil, Result{}, err
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Meta.Date != notes[j].Meta.Date {
			return notes[i].Meta.Date > notes[j].Meta.Date
		}
		return notes[i].Stem < notes[j].Stem
	})

	var b strings.Builder
	lang := l.Lang()
	title := lang.T("결정 색인", "Decision index")
	fmt.Fprintf(&b, "---\ntitle: %s\ntags: [index, decision]\n---\n\n", title)
	fmt.Fprintf(&b, "# %s\n\n> %s\n\n", title, lang.T(
		"자동 생성된다. 직접 편집하지 마라 — `prior index` 가 덮어쓴다.",
		"Generated automatically. Do not edit — `prior index` overwrites this."))

	// 요약 줄. **`아쉬운 결과` 가 이 줄의 존재 이유다** — 뒤집혔거나 나쁘게 끝난 결정이
	// 몇 건인지가 한눈에 보여야 한다. 표를 끝까지 읽어야 알 수 있으면 아무도 안 본다.
	//
	// 여기에 생성 시각(updated) 은 넣지 않는다. 넣으면 내용이 안 바뀌어도 매일 파일이
	// 달라져서, 색인의 멱등성(같은 볼트 → 같은 바이트)이 깨진다. 언제 만들었는지는
	// 파일 mtime 이 이미 알고 있다.
	active, regret := 0, 0
	for _, n := range notes {
		if n.Meta.Status == "active" || n.Meta.Status == "" {
			active++
		}
		if n.Meta.Status == "regretted" || n.Meta.Outcome == "bad" {
			regret++
		}
	}
	fmt.Fprintf(&b, lang.T(
		"전체 %d건 · active %d건 · 아쉬운 결과(regretted/bad) %d건\n\n",
		"%d total · %d active · %d regretted/bad\n\n"), len(notes), active, regret)

	b.WriteString(header(lang))
	for _, n := range notes {
		domain := "-"
		if len(n.Meta.Domain) > 0 {
			domain = n.Meta.Domain[0]
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | [[%s]] |\n",
			n.Meta.Date, domain, escapeCell(n.Meta.Summary),
			n.Meta.Status, n.Meta.Outcome, n.Stem)
	}
	return []byte(b.String()), Result{Rows: len(notes), Skipped: skipped}, nil
}

// Write 는 색인을 디스크에 쓰고 결과를 준다.
//
// 건너뛴 노트가 있어도 쓰기는 한다 — 47행짜리 색인이 색인 없음보다는 낫다.
// 대신 Result.Skipped 로 불완전함을 알린다. 에러로 바꾸지 않는 이유는
// cli/index.go 의 주석에 적었다.
func Write(l *store.Layout) (Result, error) {
	out, res, err := Build(l)
	if err != nil {
		return Result{}, err
	}
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{}, err
	}
	// **남의 파일이면 먼저 대피시킨다.**
	//
	// 색인 경로는 설정([naming] index)이 정한다. 사용자가 그 자리에 이미 손으로 쓴
	// 문서를 두고 있으면, 우리는 그것을 경고도 없이 지워 왔다 — 실측으로 재현했다
	// (188바이트의 손글씨 문서 → 색인으로 교체, 백업 없음).
	//
	// 지우지 않고, 막지도 않는다. 막으면 capture 가 통째로 실패한다(색인 갱신이
	// capture.Do 끝에 붙어 있다). `prior init` 이 남의 settings.json 을 다룰 때와 같은
	// 방식으로 — 백업하고 진행하고 알린다.
	saved, err := preserveForeign(p, l.Lang())
	if err != nil {
		return Result{}, err
	}
	res.Preserved = saved

	// WriteFileAtomic 을 쓴다 — os.WriteFile 은 기존 파일을 먼저 비운 뒤 쓰기
	// 때문에 중간에 실패하면 색인이 잘린 채로 남는다.
	if err := store.WriteFileAtomic(p, out, 0o644); err != nil {
		return Result{}, err
	}
	return res, nil
}

// indexMarker 는 우리가 만든 색인의 표식이다. Build 가 언제나 이 줄을 넣는다.
const indexMarker = "tags: [index, decision]"

// preserveForeign 은 색인 자리에 있는 **우리 것이 아닌 파일**을 백업한다.
// 백업했으면 그 경로를, 백업할 것이 없었으면 빈 문자열을 준다.
//
// 판정은 표식 한 줄로 한다. 우리 색인은 언제나 그 줄을 갖고, 사람이 쓴 문서는
// 갖지 않는다. 읽을 수 없는 파일도 남의 것으로 본다 — 모르면 보존하는 쪽으로 기운다.
func preserveForeign(path string, lang i18n.Lang) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil // 첫 생성
	}
	if err != nil {
		return "", fmt.Errorf(lang.T(
			"색인 자리의 파일을 확인할 수 없다 (%s): %w",
			"cannot inspect the file at the index path (%s): %w"), path, err)
	}
	if strings.Contains(string(data), indexMarker) {
		return "", nil // 우리가 만든 색인이다
	}

	// 이름이 겹치면 덧붙여 고른다. 백업을 덮어쓰면 대피의 의미가 없다.
	base := path + ".priorcase-replaced"
	bak := base
	for i := 1; ; i++ {
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			break
		}
		bak = fmt.Sprintf("%s-%d", base, i)
	}
	if err := store.WriteFileAtomic(bak, data, 0o644); err != nil {
		return "", fmt.Errorf(lang.T(
			"색인 자리의 파일을 대피시킬 수 없다: %w",
			"cannot move aside the file at the index path: %w"), err)
	}
	return bak, nil
}
