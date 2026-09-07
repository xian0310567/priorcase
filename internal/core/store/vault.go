package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Note 는 디스크의 결정 노트 하나다.
type Note struct {
	Path string
	Stem string
	Meta Meta
	Body []byte
}

// Links 는 이 노트가 가리키는 다른 노트 전부다 — related · supersedes · 본문 위키링크.
//
// **노트의 성질이므로 여기 산다.** 예전에는 평가 하네스가 자기 안에 갖고 있었는데,
// 통계와 라벨러가 같은 것을 필요로 하면서 세 벌이 될 참이었다. 위키링크 문법을
// 여러 곳에서 따로 다루면 한쪽만 고쳐졌을 때 **서로 다른 그래프를 보게 된다.**
//
// 중복은 그대로 둔다 — 몇 번 가리켰는지가 뜻을 갖는 자리가 있다.
func (n Note) Links() []string {
	out := make([]string, 0, len(n.Meta.Related)+len(n.Meta.Supersedes))
	add := func(s string) {
		if v := strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "[]")); v != "" {
			out = append(out, NFC(v))
		}
	}
	for _, r := range n.Meta.Related {
		add(r)
	}
	for _, s := range n.Meta.Supersedes {
		add(s)
	}
	return append(out, BodyLinks(string(n.Body))...)
}

// BodyLinks 는 본문의 위키링크 **대상 이름**만 뽑는다.
//
// ★ **이것이 위키링크를 읽는 유일한 자리다.** 예전에는 health 의 깨진 링크 검사와
// 통계가 각자 정규식을 갖고 있었고, 그 둘이 실제로 어긋났다 — 같은 볼트를 두고
// doctor 는 "링크가 전부 걸린다" 고 하고 통계는 "끊긴 링크 36건" 이라고 했다.
// **두 진단이 반대로 말하면 사람은 둘 다 안 믿는다.**
//
// 세 가지를 걸러야 옵시디언이 실제로 거는 링크와 같아진다:
//
//   - **코드 펜스 안**은 예시다. 스키마·규약 문서가 자리표시자를 거기 적는다.
//   - **인라인 코드 안**도 같다 (“ `[[새-결정-노트]]` “ 처럼). 펜스만 보고
//     인라인을 안 보면 그런 문서가 영원히 짖고, 늘 뜨는 경고는 무시를 가르친다.
//   - **별칭·앵커**는 대상이 아니다. 표 안에서는 별칭 구분자가 `\|` 로
//     이스케이프되는데, 그걸 안 끊으면 이름 끝에 역슬래시가 붙어 멀쩡한 링크가
//     전부 깨진 것으로 보인다.
func BodyLinks(body string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if codeFenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range wikiLinkRe.FindAllStringSubmatch(inlineCodeRe.ReplaceAllString(line, ""), -1) {
			if v := LinkTarget(m[1]); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// LinkTarget 은 `[[ ]]` 안에서 대상 이름만 뽑는다 (BodyLinks 의 §).
func LinkTarget(raw string) string {
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
	return NFC(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(v), `\`)))
}

var (
	// **게으른 수량자다** (`.+?`). 한 줄에 링크가 둘이면 탐욕 매칭이 둘을 하나로 삼킨다.
	wikiLinkRe   = regexp.MustCompile(`\[\[(.+?)\]\]`)
	codeFenceRe  = regexp.MustCompile("^\\s*(```|~~~)")
	inlineCodeRe = regexp.MustCompile("`[^`]*`")
)

// SkippedNote 는 List() 가 읽지 못해 건너뛴 결정 노트 하나다.
//
// Reason 은 경로를 붙이지 않은 원인 그대로다 — 경로는 Path 로 따로 들고 있으므로
// 호출자가 볼트 상대 경로로 바꿔 보여줄 수 있다.
type SkippedNote struct {
	Path   string
	Reason error
}

// LooksNewer 는 이 건너뜀이 **더 새 판이 쓴 모양** 때문인지 본다.
//
// # 왜 갈라야 하나
//
// "못 읽었다" 만 알리면 사람이 잘못 고친다. 2026-08-21 에 실제로 그랬다 — 집이
// supersedes 를 다중값으로 올리자 회사의 옛 판이 그 노트를 파싱조차 못 해
// 건너뛰었고, 세션 시작은 "N건을 읽지 못해 회수에서 빠져 있다" 만 말했다.
// 파일을 열면 YAML 은 멀쩡해 보인다. 그래서 사람이 **옛 모양으로 되돌려**
// 다중값을 강등시켰다. 신호는 있었는데 무엇을 하라는 말이 없었다.
//
// 코드 가드로는 못 막는 종류다(손 편집이다). 막을 수 있는 것은 **왜 안 읽히는지를
// 정확히 말해 주는 것**뿐이다.
//
// # 어떻게 아나
//
// yaml.v3 의 에러가 갈래를 알려 준다. `cannot unmarshal !!X into Y` 는 **아는 키에
// 모르는 모양** 이라는 뜻이고, 그건 문서 구조가 멀쩡하다는 뜻이다 — 파서가 값까지
// 도달했다. 진짜 깨진 YAML 은 그 전 단계(스트림·들여쓰기)에서 다르게 운다.
//
// 모르는 **키**는 여기 안 온다 — Meta.Extra 가 흡수한다. 그래서 남는 판 갈림 신호가
// 이 하나뿐이다.
func (s SkippedNote) LooksNewer() bool {
	if s.Reason == nil {
		return false
	}
	return strings.Contains(s.Reason.Error(), "cannot unmarshal !!")
}

// List 는 모든 결정 폴더의 노트를 경로 오름차순으로 주고, 읽기·파싱에 실패해
// 건너뛴 파일을 두 번째 값으로 함께 준다.
//
// 건너뛰는 동작 자체는 의도된 것이다 — 노트 한 건이 깨졌다고 색인 전체가
// 죽으면 안 된다(TestListSkipsBrokenFile 이 이걸 고정한다). 문제는 예전에
// 건너뛴 것을 **아무에게도 알리지 않았다**는 점이었다: 실볼트 53건 중 6건이
// 구 스키마(title/project/created/superseded-by)로 쓰여 있어 KnownFields(true)
// 파싱에서 거부됐고, `prior index` 는 아무 말 없이 47행짜리 색인을 만들었다.
// 스펙 §1.3 이 셸의 죄목으로 든 "조용히 데이터를 잃는다" 가 그대로 재현된 것이다.
//
// 반환값을 구조체 하나로 묶지 않고 셋으로 늘린 이유: 구조체면 호출자가
// res.Notes 만 쓰고 res.Skipped 를 안 보는 것이 문법적으로 티가 나지 않는다.
// 세 번째 반환값은 무시하려면 `_` 를 명시적으로 적어야 한다 — 침묵이 의식적인
// 선택이 되고, 그 자리가 grep 으로 잡힌다.
func (l *Layout) List() ([]Note, []SkippedNote, error) {
	if l.marker == "" {
		return nil, nil, errNoMarker(l.c.Naming.DecisionFile)
	}
	var notes []Note
	var skipped []SkippedNote
	for _, dir := range l.DecisionDirs() {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("결정 폴더를 읽을 수 없다 (%s): %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			// ReadDir 결과는 macOS 에서 NFD 일 수 있다. 즉시 정규화한다.
			name := NFC(e.Name())
			if !l.isDecisionFile(name) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			n, err := l.readNote(path)
			if err != nil {
				skipped = append(skipped, SkippedNote{Path: path, Reason: err})
				continue
			}
			n.Stem = strings.TrimSuffix(name, ".md")
			notes = append(notes, n)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return notes, skipped, nil
}

// errNoMarker 는 표식을 유도할 수 없는 설정에 대한 공통 에러다.
// 조용히 "결정이 하나도 없다" 로 처리하면 색인이 통째로 비어버린다.
func errNoMarker(template string) error {
	return fmt.Errorf("설정의 decision_file 템플릿에서 결정 표식을 유도할 수 없다: %q", template)
}

// isDecisionFile 은 파일명이 결정 노트 규약에 맞는지 본다. 이름은 NFC 여야 한다.
func (l *Layout) isDecisionFile(name string) bool {
	return strings.HasSuffix(name, ".md") && strings.Contains(name, l.marker)
}

// DecisionStems 는 한 도메인의 결정 폴더에 있는 노트 stem 을 준다.
// 파일을 파싱하지 않는다 — 파일명만 필요한 호출자(중복 검사)를 위한 것이다.
// 폴더가 아직 없으면 빈 목록이고 에러가 아니다.
func (l *Layout) DecisionStems(prefix string) ([]string, error) {
	if l.marker == "" {
		return nil, errNoMarker(l.c.Naming.DecisionFile)
	}
	dir, err := l.decisionsDir(prefix)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("결정 폴더를 읽을 수 없다 (%s): %w", dir, err)
	}
	var stems []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := NFC(e.Name())
		if !l.isDecisionFile(name) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(name, ".md"))
	}
	return stems, nil
}

// Read 는 노트 하나를 읽는다. 파싱 실패에는 경로를 붙인다 — 단건 호출자는
// 에러 문자열만으로 어느 파일이 문제인지 알아야 한다.
func (l *Layout) Read(path string) (Note, error) {
	n, err := l.readNote(path)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return Note{}, err // *PathError 는 이미 경로를 담고 있다
		}
		return Note{}, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

// readNote 는 Read 의 알맹이다. 에러에 경로를 덧붙이지 않는다 — List 는 경로를
// SkippedNote.Path 로 따로 들고 있어서, 여기서 붙이면 사용자에게 나가는 경고에
// 경로가 두 번(절대 경로 + 볼트 상대 경로) 찍힌다.
func (l *Layout) readNote(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	m, body, err := ParseFrontmatter(data)
	if err != nil {
		return Note{}, err
	}
	// **우리 노트인지 여기서 가른다.**
	//
	// 예전에는 `KnownFields(true)` 가 이 일을 겸했다 — 10키 밖의 키가 하나라도 있으면
	// 파싱이 실패했으므로 옛 도구가 남긴 `title/project/created` 노트가 자연히 걸러졌다.
	// 그런데 같은 규칙이 **사용자가 Obsidian 에서 넣은 `aliases:` 한 줄**도 걸러서,
	// 멀쩡한 결정을 색인·회수·review 에서 통째로 지워 버렸다.
	//
	// 이제 잉여 키는 Extra 로 받는다. 그러면 옛 스키마 노트가 **빈 결정 노트로** 통과해
	// summary 가 빈 채 색인에 들어가므로, 대신 여기서 표식을 본다.
	// `type: decision` 이 우리 노트의 유일한 표식이다(schema.Validate 와 같은 기준).
	if m.Type != "decision" {
		return Note{}, fmt.Errorf("결정 노트가 아니다 (type: %q) — 다른 도구가 남긴 형식이거나 frontmatter 가 옛 스키마다", m.Type)
	}
	stem := strings.TrimSuffix(NFC(filepath.Base(path)), ".md")
	return Note{Path: path, Stem: stem, Meta: m, Body: body}, nil
}

// Write 는 노트를 정본 형식으로 쓴다. 부모 디렉토리를 만든다.
//
// WriteFileAtomic 을 쓴다 — os.WriteFile 은 기존 파일을 먼저 비운 뒤 쓰기
// 때문에 중간에 실패하면 결정 노트가 잘린 채로 남는다. **결정 노트는 재생성할
// 원본이 없다** — 파생물이 아니라 정본이라 이 보장이 특히 중요하다.
func (l *Layout) Write(n Note) error {
	if err := os.MkdirAll(filepath.Dir(n.Path), 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(n.Path, EmitNote(n.Meta, n.Body), 0o644)
}
