package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ★★ README 가 약속하는 다운로드 파일명과 goreleaser 가 실제로 만드는 이름이
// 어긋나면 **사용자가 404 를 만난다.**
//
// 실제로 어긋났다. 릴리스를 별도 저장소로 보내려고 `release.github.name` 을
// 적었더니 goreleaser 가 그것으로 project_name 을 유추해서
// `casebook-releases_darwin_arm64.tar.gz` 를 만들었다 — README 는
// `casebook_darwin_arm64.tar.gz` 를 가리키고 있었다. 빌드는 성공하고 테스트도
// 통과하는데 링크만 죽는, 배포에서 가장 잡기 어려운 종류다.
func TestReleaseArchiveNameMatchesReadme(t *testing.T) {
	root := repoRoot(t)
	cfg := readFile(t, filepath.Join(root, ".goreleaser.yaml"))
	readme := readFile(t, filepath.Join(root, "README.md"))

	// project_name 이 명시돼 있어야 한다. 없으면 저장소 이름에서 유추되고,
	// 그 유추가 바뀌는 순간 파일명이 조용히 달라진다.
	if !regexp.MustCompile(`(?m)^project_name:\s*casebook\s*$`).MatchString(cfg) {
		t.Error(".goreleaser.yaml 에 `project_name: casebook` 이 없다 — " +
			"저장소 이름이 파일명에 새어 들어간다")
	}

	// README 가 가리키는 아카이브 이름을 뽑아 name_template 과 대조한다.
	want := regexp.MustCompile(`casebook_(darwin|linux)_(amd64|arm64)\.tar\.gz`).FindString(readme)
	if want == "" {
		t.Fatal("README 에 다운로드 파일명이 없다")
	}
	if !strings.Contains(cfg, `name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"`) {
		t.Errorf("아카이브 name_template 이 README 의 %q 형태와 맞지 않는다", want)
	}
}

// 배포물에 EULA 와 제3자 고지가 반드시 들어가야 한다. 독점 소프트웨어라 라이선스
// 동봉이 의무이고, 링크된 오픈소스의 고지 의무도 여기서 진다.
func TestReleaseArchiveCarriesLicenses(t *testing.T) {
	root := repoRoot(t)
	cfg := readFile(t, filepath.Join(root, ".goreleaser.yaml"))
	// **줄 전체로 본다.** 부분 문자열로 보면 `- LICENSE.x` 가 `- LICENSE` 에
	// 걸려서 통과한다 — 회수 매칭에서 고친 것과 같은 종류의 버그다.
	lines := map[string]bool{}
	for _, l := range strings.Split(cfg, "\n") {
		lines[strings.TrimRight(l, " \t\r")] = true
	}
	for _, f := range []string{"LICENSE", "THIRD-PARTY-NOTICES.md"} {
		if !lines["      - "+f] {
			t.Errorf("아카이브에 %s 를 안 넣는다 — 라이선스 위반이다", f)
		}
		body := readFile(t, filepath.Join(root, f))
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s 가 비었다", f)
		}
	}
}

// 소스를 공개하지 않기로 했으므로 LICENSE 가 MIT 로 되돌아가면 안 된다.
// 되돌아가면 배포물에 "누구나 재배포 가능" 이 동봉된다.
func TestLicenseIsProprietary(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "LICENSE"))
	if !strings.Contains(body, "proprietary software") {
		t.Error("LICENSE 가 독점 라이선스가 아니다 — 소스 비공개 배포와 어긋난다")
	}
	if strings.Contains(body, "MIT License") {
		t.Error("LICENSE 가 MIT 로 되돌아갔다")
	}
}

// README 가 `go install` 을 다시 권하면 안 된다 — private 저장소에서는 죽는 명령이다.
func TestReadmeDoesNotPromiseGoInstall(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "README.md"))
	install := body
	if i := strings.Index(body, "## 설치"); i >= 0 {
		install = body[i:]
		if j := strings.Index(install[6:], "\n## "); j >= 0 {
			install = install[:j+6]
		}
	}
	if strings.Contains(install, "go install github.com/") {
		t.Error("설치 절이 `go install` 을 권한다 — 소스가 private 이라 낯선 사람에게는 죽는다")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return string(b)
}
