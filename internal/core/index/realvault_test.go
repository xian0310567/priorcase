package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/store"
)

// vaultEnv 는 실볼트 대조 테스트를 켜는 환경변수다.
const vaultEnv = "CASEBOOK_TEST_VAULT"

// TestRealVault 는 스펙 §11 이 "로컬 전용" 으로 남긴 실볼트 대조다.
// CASEBOOK_TEST_VAULT 가 없으면 건너뛴다 — CI 에는 실볼트가 없고, 있어서도 안 된다
// (§11 "실볼트 사본을 저장소에 넣지 않는다": 결정 노트에 개인 내용이 들어 있다).
//
// 이 테스트가 있어야 §12 컷오버 게이트의 첫 항목("실볼트 46건을 cb index 가
// 손실·왜곡 없이 재생성")을 확인할 수단이 생긴다. 합성 픽스처는 우리가 만든
// 모양만 담고 있어서, 실볼트에만 있는 어긋남(손으로 고친 frontmatter, 옛
// 방출기가 남긴 형식, 규약을 벗어난 파일명)은 여기서만 드러난다.
//
// ⚠️ 절대 쓰지 않는다. 실볼트는 사용자의 실제 데이터고 재생성할 원본이 없다.
// store.Layout 은 쓰되 Write 계열(l.Write·index.Write)을 부르지 않는다.
// 말로만 두지 않고 아래 snapshot() 으로 전후를 대조해 실제로 안 건드렸는지 본다.
func TestRealVault(t *testing.T) {
	vault := os.Getenv(vaultEnv)
	if vault == "" {
		t.Skipf("%s 가 없다 — 실볼트 대조는 로컬 전용이다 (스펙 §11)", vaultEnv)
	}
	if fi, err := os.Stat(vault); err != nil || !fi.IsDir() {
		t.Fatalf("%s=%q 가 디렉터리가 아니다: %v", vaultEnv, vault, err)
	}

	c := realVaultConfig(t, vault)
	l := store.NewLayout(c)

	dirs := l.DecisionDirs()
	before := snapshot(t, dirs)

	// 1) 모든 결정 노트가 파싱되는지.
	//    파일을 직접 훑어 l.Read 를 건다. l.List() 도 이제 건너뛴 것을 보고하지만,
	//    여기서 독립적으로 세어 두어야 3)에서 "List 가 보고한 건너뜀" 과 대조할
	//    수 있다 — 같은 코드로 세면 그 코드가 틀렸을 때 둘 다 같이 틀린다.
	files := decisionFiles(t, dirs, l.DecisionMarker())
	if len(files) == 0 {
		t.Fatalf("결정 노트를 하나도 못 찾았다 (%s=%q, 표식=%q)", vaultEnv, vault, l.DecisionMarker())
	}
	t.Logf("실볼트 결정 노트 %d건, 결정 폴더 %d개", len(files), len(dirs))

	var parsed []store.Note
	unparsable := map[string]bool{}
	for _, p := range files {
		n, err := l.Read(p)
		if err != nil {
			unparsable[p] = true
			t.Errorf("파싱 실패: %s\n  %v\n  → 스펙 §12 컷오버 게이트 1번(실볼트 전건 재생성)이 "+
				"아직 성립하지 않는다. 코드 결함이 아니라 볼트 데이터가 구 스키마라는 뜻이므로, "+
				"이 노트의 frontmatter 를 10키 정본으로 옮겨야 한다.", l.RelPath(p), err)
			continue
		}
		parsed = append(parsed, n)
	}

	// 2) 파싱된 노트가 schema.Validate 를 통과하는지.
	for _, n := range parsed {
		if err := schema.Validate(l.DecisionMarker(), n.Stem, n.Meta); err != nil {
			t.Errorf("스키마 위반: %s\n  %v", l.RelPath(n.Path), err)
		}
	}

	// 3) index.Build 가 노트를 흘리지 않는지 — 그리고 흘린 것을 숨기지 않는지.
	//
	//    "흘리지 않는다" 는 행 수 == 파일 수 로는 표현할 수 없다. 구 스키마 노트가
	//    남아 있는 한 그 등식은 깨지고, 깨진 채로 아무 말도 안 나가는 것이 바로
	//    이 프로젝트가 없애려는 결함이다. 그래서 등식을 다시 세운다:
	//
	//        색인 행 + 건너뛴 노트 == 디스크의 결정 노트
	//
	//    이 등식이 성립하면 노트는 색인에 들어갔거나 "빠졌다고 보고됐거나" 둘 중
	//    하나이고, 조용히 사라진 것은 하나도 없다.
	out, res, err := Build(l)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, s := range res.Skipped {
		t.Logf("색인에서 빠짐(보고됨): %s\n  %v", l.RelPath(s.Path), s.Reason)
	}
	if res.Rows+len(res.Skipped) != len(files) {
		t.Errorf("색인 행 %d + 건너뜀 %d = %d ≠ 디스크의 결정 노트 %d — 노트가 조용히 사라졌다",
			res.Rows, len(res.Skipped), res.Rows+len(res.Skipped), len(files))
	}
	// 건너뜀 보고가 실제 파싱 실패와 정확히 같은 집합인지. 개수만 맞고 대상이
	// 다르면 엉뚱한 파일을 지목하는 경고가 사용자에게 나간다.
	if len(res.Skipped) != len(unparsable) {
		t.Errorf("Build 가 보고한 건너뜀 = %d건, 실제 파싱 실패 = %d건", len(res.Skipped), len(unparsable))
	}
	for _, s := range res.Skipped {
		if !unparsable[s.Path] {
			t.Errorf("파싱되는 노트를 건너뛴 것으로 보고했다: %s", l.RelPath(s.Path))
		}
	}
	if rows := countRows(string(out)); rows != res.Rows {
		t.Errorf("색인 표의 행 수 = %d, Build 가 센 행 = %d — 방출 단계에서 유실됐다", rows, res.Rows)
	}

	// 4) 읽기만 했는지.
	if after := snapshot(t, dirs); after != before {
		t.Errorf("실볼트가 변경됐다 — 이 테스트는 읽기 전용이어야 한다\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// realVaultConfig 는 실볼트를 가리키는 설정을 만든다.
//
// 사용자의 실제 설정 파일을 읽지 않는다 — 그 파일이 없어도(지금이 그렇다) 이
// 테스트는 돌아야 하고, 있더라도 그쪽 vault 를 실수로 쓰게 되면 안 된다.
// 도메인 목록은 볼트를 훑어 유도한다: `<folder>/decisions` 가 있으면 그 folder 가
// 도메인이다 — 기본 decisions_dir 템플릿("{project}/decisions")의 역연산이다.
func realVaultConfig(t *testing.T, vault string) *config.Config {
	t.Helper()
	c := &config.Config{
		Vault: vault,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
	}
	err := filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 못 읽는 가지는 건너뛴다 — 볼트에는 우리 것이 아닌 폴더도 있다
		}
		if !d.IsDir() {
			return nil
		}
		if p != vault && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir // .obsidian·.git 등
		}
		if d.Name() != "decisions" {
			return nil
		}
		folder, err := filepath.Rel(vault, filepath.Dir(p))
		if err != nil || folder == "." {
			return nil
		}
		c.Domain = append(c.Domain, config.Domain{Prefix: folder, Folder: folder})
		return fs.SkipDir
	})
	if err != nil {
		t.Fatalf("볼트를 훑을 수 없다 (%s): %v", vault, err)
	}
	if len(c.Domain) == 0 {
		t.Fatalf("결정 폴더(<도메인>/decisions)를 하나도 못 찾았다: %s", vault)
	}
	return c
}

// decisionFiles 는 결정 폴더의 노트 파일 경로를 전부 준다. 파싱하지 않는다 —
// "파싱되는 것만" 세면 파싱 실패가 통계에서 사라진다.
func decisionFiles(t *testing.T, dirs []string, marker string) []string {
	t.Helper()
	if marker == "" {
		t.Fatal("결정 표식이 비었다")
	}
	var out []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("결정 폴더를 읽을 수 없다 (%s): %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := store.NFC(e.Name())
			if strings.HasSuffix(name, ".md") && strings.Contains(name, marker) {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	return out
}

// countRows 는 색인 markdown 의 데이터 행 수를 센다. 헤더 두 줄은 뺀다.
func countRows(md string) int {
	n := 0
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		if strings.HasPrefix(line, "| 날짜 |") || strings.HasPrefix(line, "| --- |") {
			continue
		}
		n++
	}
	return n
}

// snapshot 은 결정 폴더의 파일 이름·크기·수정시각을 한 문자열로 만든다.
// 테스트 전후를 대조해 "읽기만 했다"를 주장이 아니라 관측으로 만든다.
func snapshot(t *testing.T, dirs []string) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil {
				continue
			}
			b.WriteString(dir + "/" + store.NFC(e.Name()) + " " +
				fi.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z") + " " +
				strconv.FormatInt(fi.Size(), 10) + "\n")
		}
	}
	return b.String()
}
