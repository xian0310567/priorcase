package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// buildCB 는 실제 바이너리를 만든다. 패키지 안에서 서버를 조립하는 테스트와 달리
// 여기서는 **호스트가 보는 것과 같은 프로세스**를 검증해야 한다 — cobra 배선,
// 설정 로딩, stdio 전송이 전부 실물이어야 의미가 있다.
func buildCB(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "prior")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/xian0310567/priorcase/cmd/prior")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prior 빌드 실패: %v\n%s", err, out)
	}
	return bin
}

// stdio 전송은 **개행 구분 JSON** 이다. prior mcp 가 도는 동안 stdout 에 프로토콜이
// 아닌 것이 한 줄이라도 섞이면 프레이밍이 깨져 이 핸드셰이크가 실패한다.
//
// 깨진 노트가 있는 볼트를 일부러 쓴다. 건너뜀 경고는 CLI 에서 stderr 로 나가는
// 정보인데, 누군가 그 경로를 stdout 으로 바꾸면 정확히 여기서 세션이 죽는다.
// 경고가 가장 많이 나는 상황을 골라야 이 테스트가 그 회귀를 잡는다.
func TestMCPServerSpeaksCleanStdio(t *testing.T) {
	bin := buildCB(t)
	cfgPath, c := testutil.VaultConfigFile(t)

	broken := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	cmd := exec.Command(bin, "--config", cfgPath, "mcp")
	var stderr strings.Builder
	cmd.Stderr = &stderr

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "v0"}, nil).
		Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("핸드셰이크 실패 (stdout 오염 의심): %v\nstderr:\n%s", err, stderr.String())
	}
	defer func() { _ = cs.Close() }()

	if ins := cs.InitializeResult().Instructions; !strings.Contains(ins, "priorcase_recall") {
		t.Errorf("실 바이너리의 instructions 에 행동 계약이 없다:\n%s", ins)
	}

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "priorcase_recall", Arguments: map[string]any{"query": "저장 엔진"},
	})
	if err != nil {
		t.Fatalf("도구 호출 실패: %v\nstderr:\n%s", err, stderr.String())
	}
	var out strings.Builder
	for _, ct := range res.Content {
		if tc, ok := ct.(*sdk.TextContent); ok {
			out.WriteString(tc.Text)
		}
	}
	if !strings.Contains(out.String(), "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("실 바이너리 회수 결과가 비었다:\n%s", out.String())
	}
	// 건너뛴 노트가 응답 본문으로 나와야 한다 — stderr 로 갔으면 에이전트는 모른다.
	if !strings.Contains(out.String(), "깨짐") {
		t.Errorf("건너뛴 노트가 응답에 없다:\n%s", out.String())
	}
}
