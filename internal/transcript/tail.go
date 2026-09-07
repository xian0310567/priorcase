package transcript

import (
	"bytes"
	"io"
	"os"
)

// Tail 은 기록 파일의 **끝부분만** 읽는다.
//
// # 왜 통째로 안 읽는가
//
// 회수는 매 프롬프트마다 돈다. 그 자리에서 파일 전체를 파싱하면 긴 대화일수록
// 느려지는데, 하필 긴 대화가 맥락이 가장 필요한 자리다. 이 기계 실측으로
// Claude Code 기록 1,492개의 중앙값은 82KB 지만 p90 이 128KB, **최대 51MB** 다.
// 51MB 를 매 발화마다 훑으면 그 세션은 못 쓴다.
//
// 끝부분만 읽어도 되는 이유는 필요한 것이 **직전 대화**이기 때문이다. 회수가
// 맥락에서 얻으려는 것은 "지금 무슨 얘기 중인가" 이지 세션 전체의 역사가 아니다.
//
// # 첫 줄은 버린다
//
// 임의의 자리에서 자르면 첫 줄은 반쪽이다. 반쪽 JSON 은 파서가 깨진 줄로 세는데,
// 그 수는 체크포인트 전진을 막는 신호로도 쓰인다(스펙 §7.2) — 여기서 흘려보내면
// 정상인 파일이 계속 깨진 것으로 보인다. 그래서 **여기서 잘라 내고 넘긴다.**
//
// 파일이 상한보다 작으면 자를 것이 없으므로 통째로 준다 — 그때는 첫 줄이 진짜
// 첫 줄이라 버리면 안 된다.
//
// # consumed 를 쓰면 안 된다
//
// 이 함수가 준 Reader 로 Parse 를 부르면 `consumed` 는 **잘라 낸 뒤의** 바이트
// 수다. 파일 처음부터의 오프셋이 아니므로 체크포인트에 쓰면 기록을 통째로
// 되감는다. 이 함수는 읽기 전용 훑기(회수 맥락)에만 쓴다.
func Tail(path string, maxBytes int64) (io.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	from, whole := int64(0), true
	if st.Size() > maxBytes {
		from, whole = st.Size()-maxBytes, false
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if !whole {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil // 상한 안에 개행이 하나도 없다 — 온전한 줄이 없다는 뜻이다
		}
	}
	return bytes.NewReader(b), nil
}
