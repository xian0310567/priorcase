package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ledgerFile 은 승격 원장이다. **덧붙이기 전용**이다.
//
// state.json 에 넣지 않는 이유가 둘이다. 하나, 상태 파일은 매 스캔마다 통째로
// 다시 쓰이므로 이력이 쌓이면 그 비용이 선형으로 는다. 둘, 상태 파일이 깨져서
// 사람이 지우면 이력까지 같이 사라지는데 — 그때가 바로 "그동안 뭐가 돌았나" 를
// 알아야 하는 순간이다.
//
// 사람이 언제든 지워도 된다. 이건 진단용 로그이고, 아무것도 이걸 정본으로 읽지 않는다.
const ledgerFile = "promotions.jsonl"

// Promotion 은 승격 시도 하나의 기록이다.
//
// **성공만 남기면 안 된다.** "판별기가 봤는데 기록할 게 아니라고 했다" 와 "판별기가
// 아예 안 돌았다" 는 완전히 다른 사실인데, 성공만 남기면 둘 다 빈 원장으로 보인다.
// 컷오버 1일차 회고에서 prior doctor 가 정확히 그 둘을 구분하지 못했다.
type Promotion struct {
	At       time.Time `json:"at"`
	ID       string    `json:"id"`
	Domain   string    `json:"domain"`
	Recorded bool      `json:"recorded"`
	Path     string    `json:"path,omitempty"`   // 만들어진 노트 (Recorded 일 때)
	Reason   string    `json:"reason,omitempty"` // 안 만든 이유 (판별기가 준 것)
	Err      string    `json:"err,omitempty"`    // 판별기를 못 불렀을 때
}

// AppendPromotion 은 승격 기록 한 줄을 덧붙인다.
//
// 실패해도 승격 자체를 되돌리지 않는다 — 원장은 진단용이고, 그것 때문에 기록이
// 멎으면 본말이 뒤집힌다. 호출자는 에러를 stderr 로 흘리기만 하면 된다.
func AppendPromotion(dir string, p Promotion) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ledgerFile),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadPromotions 는 원장을 오래된 순으로 준다. since 가 제로가 아니면 그 이후만.
//
// 깨진 줄은 건너뛴다 — 덧붙이는 도중에 죽으면 마지막 줄이 잘릴 수 있고, 그것 때문에
// 진단이 통째로 실패하면 안 된다. 파일이 없으면 빈 목록이다(승격이 한 번도 없었다).
func ReadPromotions(dir string, since time.Time) ([]Promotion, error) {
	f, err := os.Open(filepath.Join(dir, ledgerFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("승격 원장을 읽을 수 없다: %w", err)
	}
	defer f.Close()

	var out []Promotion
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var p Promotion
		if json.Unmarshal(sc.Bytes(), &p) != nil {
			continue
		}
		if !since.IsZero() && p.At.Before(since) {
			continue
		}
		out = append(out, p)
	}
	return out, sc.Err()
}

// maxLedgerText 는 원장 한 줄에 담는 자유 문자열의 상한이다.
//
// 판별기 실패 메시지에는 호스트 CLI 의 stderr 가 통째로 들어올 수 있다 (MCP 스택
// 트레이스, 대량 경고). 그것이 ReadPromotions 의 스캐너 상한(1MB)을 넘으면 **그 줄만
// 사라지는 것이 아니라 그 뒤가 통째로 안 읽힌다** — 그리고 하필 그 순간이 진단이
// 가장 필요한 순간이다. 이 프로젝트가 죄목으로 드는 침묵 무동작 그 자체다.
const maxLedgerText = 2000

// TrimLedgerText 는 원장에 넣을 문자열을 상한까지 자른다. 앞을 남긴다 — 오류
// 메시지는 첫 줄에 원인이 있다.
func TrimLedgerText(s string) string {
	r := []rune(s)
	if len(r) <= maxLedgerText {
		return s
	}
	return string(r[:maxLedgerText]) + "…(잘림)"
}
