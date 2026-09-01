package arch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ★★ 아카이브 이름을 유추에 맡기면 조용히 바뀐다.
//
// 한때 릴리스를 별도 저장소로 보내려고 `release.github.name` 을 적었더니 goreleaser 가
// 그것으로 project_name 을 유추해 `priorcase-releases_darwin_arm64.tar.gz` 를 만들었다.
// 빌드도 테스트도 `goreleaser check` 도 통과하고 **다운로드 링크만 죽는** 종류다.
//
// 지금은 npm 이 배포 경로라 그 링크가 README 에 없지만, `scripts/npm-pack.sh` 가
// 이 파일명에 의존한다 — 이름이 바뀌면 npm 패키징이 통째로 깨진다.
func TestArchiveNameIsPinned(t *testing.T) {
	root := repoRoot(t)
	cfg := readFile(t, filepath.Join(root, ".goreleaser.yaml"))
	if !regexp.MustCompile(`(?m)^project_name:\s*priorcase\s*$`).MatchString(cfg) {
		t.Error(".goreleaser.yaml 에 `project_name: priorcase` 이 없다 — " +
			"저장소 이름이 파일명에 새어 들어간다")
	}
	if !strings.Contains(cfg, `name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"`) {
		t.Error("아카이브 name_template 이 바뀌었다")
	}
	// npm-pack.sh 가 기대하는 이름과 실제로 맞는지 본다.
	pack := readFile(t, filepath.Join(root, "scripts", "npm-pack.sh"))
	if !strings.Contains(pack, `priorcase_${goos}_${goarch}.tar.gz`) {
		t.Error("npm-pack.sh 가 기대하는 아카이브 이름이 goreleaser 와 어긋난다")
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

// README 가 `go install` 을 다시 권하면 안 된다 — 소스가 private 이라 죽는 명령이다.
// 그리고 npm 이 1차 경로라는 것이 설치 절에 있어야 한다.
func TestReadmeInstallPathIsNpm(t *testing.T) {
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
	for _, want := range []string{"npm install -g priorcase", "npx", "mcpServers"} {
		if !strings.Contains(install, want) {
			t.Errorf("설치 절에 %q 가 없다 — npm 이 1차 경로다", want)
		}
	}
	// 훅이 npx 로 안 된다는 사실을 반드시 적어야 한다. 안 적으면 사용자가
	// mcpServers 만 걸고 훅이 도는 줄 안다.
	if !strings.Contains(install, "npx` 로는 훅이 안 된다") {
		t.Error("npx 로 훅이 안 된다는 경고가 없다 — 사용자가 자동 기록이 도는 줄 안다")
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

// ★ **릴리스가 npm 토큰으로 되돌아가면 안 된다.**
//
// 2026-08-10 에 옛 이름으로 첫 게시를 하며 알게 된 것: 클래식 토큰은 폐지됐고,
// TOTP 2FA 는 등록 자체가 사라졌고, 보안 키는 CLI 에서 6자리 코드를 못 만든다.
// 남은 길이 "Granular token + Bypass 2FA 를 만들어 한 번 쓰고 폐기" 뿐이었다.
// 트러스티드 퍼블리싱(OIDC)이 그 절차 전체를 없앤다.
//
// 되돌아가는 방식이 둘이라 둘 다 막는다.
//   - `NODE_AUTH_TOKEN`/`NPM_TOKEN` 이 다시 들어오는 것 (옛 워크플로를 복붙)
//   - `id-token: write` 가 빠지는 것 — 이쪽이 더 나쁘다. 권한만 없으면 npm 은
//     "인증이 없다" 고 하는데, 그 메시지가 토큰 문제처럼 보여서 사람이 다시
//     토큰을 넣게 만든다.
func TestReleaseUsesTrustedPublishingNotTokens(t *testing.T) {
	root := repoRoot(t)
	wf := readFile(t, filepath.Join(root, ".github/workflows/release.yml"))

	for _, bad := range []string{"NODE_AUTH_TOKEN", "NPM_TOKEN"} {
		if strings.Contains(wf, bad) {
			t.Errorf("release.yml 에 %s 가 돌아왔다 — 게시는 OIDC 로 한다. "+
				"장기 토큰을 레포 시크릿에 두지 않기로 했다", bad)
		}
	}
	// 줄 끝 주석을 허용한다 — 왜 이 권한이 있는지 적어 두는 편이 낫고,
	// 주석 때문에 검사가 헛도는 것이 더 나쁘다.
	if !regexp.MustCompile(`(?m)^\s*id-token:\s*write\s*(#.*)?$`).MatchString(wf) {
		t.Error("release.yml 에 `id-token: write` 가 없다 — OIDC 토큰이 발급되지 않아 " +
			"게시가 인증 실패로 죽는다 (그 에러는 토큰 문제처럼 보인다)")
	}

	// **node 22.14 미만이면 npm 이 10.x 라 OIDC 교환을 못 한다.** setup-node 의
	// node-version 은 문자열이라 오타나 옛 값이 조용히 통과한다.
	m := regexp.MustCompile(`node-version:\s*'(\d+)'`).FindStringSubmatch(wf)
	if m == nil {
		t.Fatal("release.yml 에서 node-version 을 찾지 못했다")
	}
	if major := m[1]; major < "22" || len(major) < 2 {
		t.Errorf("node-version 이 %q 다 — 트러스티드 퍼블리싱은 node 22.14 이상, "+
			"npm 11.5.1 이상을 요구한다", major)
	}
}

// ★ **회고 큐는 편승 회수를 그대로 재현해야 한다.**
//
// retro.Due 는 "그 결정을 기록하던 순간 무엇이 함께 보였나" 를 다시 계산한다.
// capture 가 쓰는 회수 조건(Limit·MinScore)과 값이 갈리면 그건 재현이 아니라 다른
// 계산이고, 그러면 회고 큐가 실제로는 아무도 못 본 관계를 근거로 사람에게 묻게 된다.
//
// 두 값은 서로 다른 패키지에 있어서 한쪽만 고쳐도 컴파일이 통과한다 — 그래서 검사다.
func TestRetroMirrorsCaptureRecallOptions(t *testing.T) {
	root := repoRoot(t)
	cap := readFile(t, filepath.Join(root, "internal/core/capture/capture.go"))
	rt := readFile(t, filepath.Join(root, "internal/core/retro/retro.go"))

	m := regexp.MustCompile(`search\.Options\{CrossProject: true, Limit: (\d+), MinScore: (\d+)\}`).
		FindStringSubmatch(cap)
	if m == nil {
		t.Fatal("capture 의 편승 회수 옵션을 찾지 못했다 — 형태가 바뀌었으면 이 검사도 같이 고쳐라")
	}

	// ★ **Cwd 를 주는지까지 봐야 한다.** 처음 이 검사를 쓸 때 Limit·MinScore 만 보고
	// Cwd 를 빠뜨렸고, 그래서 실제로 어긋난 것을 못 잡았다. cwd 도메인에는 +4 가
	// 붙어서 다른 프로젝트의 결정이 상위 3에서 통째로 밀려난다 — 실측에서 큐가
	// 26건과 33건으로 갈렸고 빠진 7건이 전부 다른 프로젝트 것이었다.
	if strings.Contains(m[0], "Cwd") {
		t.Error("capture 가 Cwd 를 주기 시작했다 — retro 도 같이 줘야 한다")
	}
	if regexp.MustCompile(`(?s)search\.Recall\(l, c,.*?Cwd:`).MatchString(rt) {
		t.Error("retro 가 Cwd 를 준다 — capture 는 안 준다. cwd 도메인에 +4 가 붙어 " +
			"다른 프로젝트의 결정이 상위 3에서 밀려나고, 큐가 그만큼 조용해진다")
	}

	// **질의 구성도 같아야 한다.** capture 는 summary 와 slug 를 붙여 던진다.
	// retro 가 summary 만 던지면 점수가 달라져 재현이 아니게 된다.
	if !strings.Contains(cap, `r.Summary+" "+r.Slug`) {
		t.Error("capture 의 편승 질의 구성이 바뀌었다 — 이 검사도 같이 고쳐라")
	}
	if !strings.Contains(rt, `n.Meta.Summary+" "+slugOf(l, n.Stem)`) {
		t.Error("retro 의 질의가 capture 와 다르다 — summary 와 slug 를 같이 던져야 한다")
	}
	for _, want := range []struct{ konst, val string }{
		{"recallLimit", m[1]},
		{"recallMinScore", m[2]},
	} {
		re := regexp.MustCompile(want.konst + `\s*=\s*(\d+)`)
		got := re.FindStringSubmatch(rt)
		if got == nil {
			t.Errorf("retro 에 %s 가 없다", want.konst)
			continue
		}
		if got[1] != want.val {
			t.Errorf("retro.%s = %s 인데 capture 는 %s 를 쓴다 — 회고 큐가 편승 회수를 "+
				"재현하지 못하고, 아무도 못 본 관계를 근거로 묻게 된다", want.konst, got[1], want.val)
		}
	}
}

// ★ 윈도우 배포가 조용히 빠지지 않게 한다.
//
// 사내에 윈도우 기획자가 있고 그 사람도 Claude Code 로 작업하고 기록한다
// (2026-08-31). goos 에서 windows 가 빠지면 npm-pack.sh 가 "없다: …zip" 으로
// 죽어서 릴리스가 멎기는 하는데, 그때는 태그를 이미 민 뒤다.
func TestReleaseBuildsWindows(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), ".goreleaser.yaml"))
	if !strings.Contains(body, "goos: [darwin, linux, windows]") {
		t.Error(".goreleaser.yaml 의 goos 에 windows 가 없다 — 윈도우 팀원이 못 받는다")
	}
	// **윈도우 산출물은 zip 이어야 한다.** tar.gz 는 탐색기가 기본으로 못 열고,
	// npm-pack.sh 가 zip 을 기대한다(unzip).
	if !strings.Contains(body, "formats: [zip]") {
		t.Error("윈도우 아카이브가 zip 이 아니다 — npm-pack.sh 의 unzip 이 못 찾는다")
	}
	// 게시 루프가 win32 패키지를 안 집으면 만들어 놓고 안 올린다.
	wf := readFile(t, filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	if !strings.Contains(wf, "dist/npm/win32-*") {
		t.Error("release.yml 의 게시 루프가 win32 패키지를 안 집는다 — 만들고 안 올린다")
	}
}

// ★ **앱 빌드가 사이드카를 매번 새로 만들어야 한다.**
//
// 2026-09-01 사고: `tauri build` 만 돌려서 번들된 prior 가 낡은 채로 남았고,
// 그 판이 `show --json` 의 새 키를 안 내서 앱이 **검은 화면**이 됐다. 사람은
// 원인을 알 길이 없었다 — 앱도 CLI 도 각자는 멀쩡했다.
//
// 사이드카는 소스에서 만들어지므로 안 만들면 **조용히 낡는다.** beforeBuildCommand
// 가 부르는 자리에 끼워 그 창을 없앤다.
func TestAppBuildRegeneratesSidecar(t *testing.T) {
	raw := readFile(t, filepath.Join(repoRoot(t), "app", "package.json"))
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Scripts["build"], "bundle-prior.sh") {
		t.Errorf("app 의 build 스크립트가 사이드카를 안 만든다: %q\n"+
			"  그러면 번들된 prior 가 조용히 낡고, 앱과 판이 갈리는 순간 화면이 죽는다",
			p.Scripts["build"])
	}
	// tauri.conf 의 beforeBuildCommand 가 그 스크립트를 부르는지도 본다 —
	// package.json 만 고치고 tauri 가 다른 것을 부르면 아무 일도 안 일어난다.
	conf := readFile(t, filepath.Join(repoRoot(t), "app", "src-tauri", "tauri.conf.json"))
	if !strings.Contains(conf, `"beforeBuildCommand": "pnpm build"`) {
		t.Error("tauri 의 beforeBuildCommand 가 pnpm build 가 아니다 — 사이드카 생성이 안 걸린다")
	}
}
