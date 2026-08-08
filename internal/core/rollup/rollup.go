// Package rollup 은 작업 로그를 주 단위로 묶는다.
//
// **LLM 을 부르지 않는다.** 옛 셸 구현(`rollup.sh`)은 `claude -p` 로 요약문을 만들었는데,
// 그건 [[casebook-결정-기록회수모델-에이전트주도-2026-08-07]] 이 데몬에서 걷어낸 것과
// 같은 의존이다 — 키 등록이 오픈소스 진입 장벽이고, 어차피 세션에 도는 에이전트가 이미
// 전체 맥락을 갖고 있다.
//
// 그래서 역할을 나눈다. **casebook 은 기계적인 것만 한다** — 어느 주가 남았는지 찾고,
// 그 주의 블록을 뽑고, 중복 없이 붙인다. **판단(요약문)은 에이전트가 쓴다.**
// `cb capture` 와 같은 구조다: 파일 규약은 우리가, 산문은 에이전트가.
package rollup

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/store"
)

// minBlockBytes 보다 작은 주는 건너뛴다. 한 줄짜리 로그를 요약해 봐야 원본보다 길다.
const minBlockBytes = 100

// dateHeading 은 작업 로그의 날짜 헤딩이다 (`## YYYY-MM-DD — 한 줄 요약`).
var dateHeading = regexp.MustCompile(`(?m)^## (\d{4}-\d{2}-\d{2})`)

// weekHeading 은 요약 파일의 주 헤딩이다 (`## 2026-W32`).
var weekHeading = regexp.MustCompile(`(?m)^## (\d{4}-W\d{2})\s*$`)

// anyH2 는 아무 h2 헤딩이다. **블록의 끝을 여기서 끊는다.**
//
// 날짜 헤딩만 경계로 쓰면, 마지막 날짜 뒤에 오는 `## 관련 문서` 같은 절이 그 주
// 블록에 통째로 삼켜진다 — 실볼트의 mesh 로그에서 실제로 그랬다. 그 절은 어느 주의
// 작업도 아니다. `###` 은 `## ` 로 시작하지 않으므로 하위 절은 안 걸린다.
var anyH2 = regexp.MustCompile(`(?m)^## `)

// Week 은 한 프로젝트의 한 주다.
type Week struct {
	Prefix  string
	Week    string // 2026-W32
	Bytes   int    // 그 주 블록의 크기
	Done    bool   // 이미 요약돼 있다
	Current bool   // 진행 중인 주 — 아직 요약하지 않는다
	Short   bool   // 내용이 너무 적다
}

// Todo 는 지금 요약해야 하는 주인지 알려 준다.
func (w Week) Todo() bool { return !w.Done && !w.Current && !w.Short }

// isoWeek 는 시각을 `YYYY-Www` 로 만든다.
func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// Scan 은 선언된 모든 도메인의 작업 로그를 훑어 주 목록을 만든다.
//
// 작업 로그가 없는 도메인은 조용히 건너뛴다 — 아직 아무 일도 안 한 프로젝트가 정상이다.
// 다만 **읽을 수는 있는데 실패한 것**은 알린다(반환 에러). 침묵하면 그 프로젝트의
// 요약이 영영 안 생기는데 이유를 알 수 없다.
func Scan(l *store.Layout, now time.Time) ([]Week, error) {
	current := isoWeek(now)
	var out []Week

	for _, prefix := range l.Prefixes() {
		logPath, err := l.WorklogPath(prefix)
		if err != nil {
			continue // 알 수 없는 접두어 — 설정 검증이 잡을 일이다
		}
		body, err := os.ReadFile(logPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("작업 로그를 읽을 수 없다 (%s): %w", l.RelPath(logPath), err)
		}

		blocks, err := weekBlocks(string(body))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.RelPath(logPath), err)
		}
		if len(blocks) == 0 {
			continue
		}

		done, err := summarized(l, prefix)
		if err != nil {
			return nil, err
		}

		for _, wk := range sortedKeys(blocks) {
			out = append(out, Week{
				Prefix:  prefix,
				Week:    wk,
				Bytes:   len(blocks[wk]),
				Done:    done[wk],
				Current: wk == current,
				Short:   len(blocks[wk]) < minBlockBytes,
			})
		}
	}
	return out, nil
}

// Block 은 한 주의 로그 원문을 준다. 에이전트가 이걸 읽고 요약문을 쓴다.
func Block(l *store.Layout, prefix, week string) (string, error) {
	logPath, err := l.WorklogPath(prefix)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("작업 로그를 읽을 수 없다 (%s): %w", l.RelPath(logPath), err)
	}
	blocks, err := weekBlocks(string(body))
	if err != nil {
		return "", err
	}
	b, ok := blocks[week]
	if !ok {
		return "", fmt.Errorf("%s 의 작업 로그에 %s 주가 없다", prefix, week)
	}
	return b, nil
}

// Append 는 요약문을 요약 파일에 붙인다. **원본 작업 로그는 손대지 않는다.**
//
// 같은 주가 이미 있으면 거부한다 — 덮어쓰면 앞서 쓴 요약이 조용히 사라진다.
func Append(l *store.Layout, prefix, week, summary string) (string, error) {
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("요약문이 비었다")
	}
	if !weekHeading.MatchString("## " + week) {
		return "", fmt.Errorf("주 형식이 아니다 (YYYY-Www 여야 한다): %q", week)
	}
	path, err := l.RollupPath(prefix)
	if err != nil {
		return "", err
	}

	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		existing = []byte(header(l, prefix))
	case err != nil:
		return "", fmt.Errorf("요약 파일을 읽을 수 없다 (%s): %w", l.RelPath(path), err)
	default:
		for _, m := range weekHeading.FindAllStringSubmatch(string(existing), -1) {
			if m[1] == week {
				return "", fmt.Errorf("%s 는 이미 요약돼 있다 (%s) — 덮어쓰지 않는다",
					week, l.RelPath(path))
			}
		}
	}

	out := string(existing)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += fmt.Sprintf("\n## %s\n\n%s\n", week, strings.TrimRight(summary, "\n"))
	if err := store.WriteFileAtomic(path, []byte(out), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func header(l *store.Layout, prefix string) string {
	logPath, _ := l.WorklogPath(prefix)
	return fmt.Sprintf(`---
title: %s 작업 로그 주간 요약
type: rollup
tags: [rollup, %s]
---

# %s — 작업 로그 주간 요약

> `+"`cb rollup`"+` 이 %s 를 주 단위로 묶는다. **원본은 그대로 남는다.**
> 요약문은 에이전트가 쓴다 — casebook 은 블록을 뽑고 붙이는 일만 한다.
`, prefix, prefix, prefix, l.RelPath(logPath))
}

// summarized 는 이미 요약된 주를 준다.
func summarized(l *store.Layout, prefix string) (map[string]bool, error) {
	out := map[string]bool{}
	path, err := l.RollupPath(prefix)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("요약 파일을 읽을 수 없다 (%s): %w", l.RelPath(path), err)
	}
	for _, m := range weekHeading.FindAllStringSubmatch(string(body), -1) {
		out[m[1]] = true
	}
	return out, nil
}

// weekBlocks 는 작업 로그를 주별 원문으로 쪼갠다.
//
// 날짜 헤딩(`## YYYY-MM-DD`)을 경계로 삼고, 각 날짜가 속한 ISO 주에 붙인다.
// 첫 날짜 헤딩 앞의 머리말은 어느 주에도 속하지 않으므로 버린다.
func weekBlocks(body string) (map[string]string, error) {
	locs := dateHeading.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		return nil, nil
	}
	bounds := anyH2.FindAllStringIndex(body, -1)

	blocks := map[string]*strings.Builder{}
	for _, loc := range locs {
		date := body[loc[2]:loc[3]]
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			// 정규식이 이미 형식을 걸렀으므로 여기 오는 것은 2026-02-31 같은 날짜다.
			return nil, fmt.Errorf("작업 로그의 날짜 헤딩이 실제 날짜가 아니다: %q", date)
		}
		end := nextBoundary(bounds, loc[0], len(body))

		wk := isoWeek(t)
		if blocks[wk] == nil {
			blocks[wk] = &strings.Builder{}
		}
		blocks[wk].WriteString(body[loc[0]:end])
	}
	out := make(map[string]string, len(blocks))
	for k, v := range blocks {
		out[k] = strings.TrimRight(v.String(), "\n") + "\n"
	}
	return out, nil
}

// nextBoundary 는 start 다음에 오는 h2 헤딩의 시작 위치다. 없으면 eof.
func nextBoundary(bounds [][]int, start, eof int) int {
	for _, b := range bounds {
		if b[0] > start {
			return b[0]
		}
	}
	return eof
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// 주 문자열은 YYYY-Www 라 사전순 = 시간순이다.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
