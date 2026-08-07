package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	s := strings.Repeat("한글결정", 50) // 200 rune / 600 byte (한글결정)
	for n := 0; n <= 210; n++ {
		got := TruncateRunes(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d 에서 깨진 UTF-8", n)
		}
		want := n
		if want > 200 {
			want = 200
		}
		if c := utf8.RuneCountInString(got); c != want {
			t.Fatalf("n=%d: rune 수 %d, want %d", n, c, want)
		}
	}
}

// TestTruncateRunesCombiningAndEmoji 는 결합문자(NFD 자모)와 4바이트 이모지가
// 섞인 문자열의 모든 접두사에서 유효한 UTF-8 이 나오는지 확인한다.
// rune 경계로 자르면 grapheme(예: 이모지+피부톤 수정자)이 쪼개질 수 있는데,
// 이는 알려진 한계이고 파일명 용도로는 허용된다 — 여기서 검증할 것은
// grapheme 보존이 아니라 "유효한 UTF-8 이 나오는가" 뿐이다.
func TestTruncateRunesCombiningAndEmoji(t *testing.T) {
	// NFD 자모와 결합 악센트는 TestNFC 처럼 코드포인트 이스케이프로 쓴다.
	// 소스에 실제 문자로 적으면 에디터·git 필터·머지 도구가 NFC 로 정규화해도
	// 테스트가 그대로 PASS 해버려 이 케이스의 커버리지가 조용히 사라지므로,
	// TestNFC 처럼 NFC 형태와 다르다는 선행 단언을 둔다.
	hangulNFD := "\u1112\u1161\u11ab" // 한 (NFD, 초성+중성+종성)
	hangulNFC := "\ud55c"             // 한 (NFC)
	if hangulNFD == hangulNFC {
		t.Fatal("한 NFD 리터럴이 NFC 와 같다 — 테스트가 무의미하다")
	}
	eNFD := "e\u0301" // e + 결합 악센트(U+0301) 로 만든 é 의 NFD
	eNFC := "\u00e9"  // é 의 NFC
	if eNFD == eNFC {
		t.Fatal("e NFD 리터럴이 NFC 와 같다 — 테스트가 무의미하다")
	}
	// 나머지는 4바이트 이모지: 피부톤 수정자 붙은 👍🏽, 일반 이모지 😀,
	// 지역표시 문자 쌍으로 만들어진 국기 🇰🇷.
	s := hangulNFD + eNFD + "\U0001F44D\U0001F3FD\U0001F600\U0001F1F0\U0001F1F7"
	n := utf8.RuneCountInString(s)
	for i := 0; i <= n+5; i++ {
		got := TruncateRunes(s, i)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d 에서 깨진 UTF-8: %q", i, got)
		}
	}
}

func TestNFC(t *testing.T) {
	// NFD 는 반드시 코드포인트 이스케이프로 쓴다. 소스에 한글을 직접 적으면
	// 에디터·도구가 NFC 로 정규화해 테스트가 같은 문자열끼리 비교하며 무의미해진다.
	nfd := "\u1112\u1161\u11ab" // U+1112 + U+1161 + U+11AB
	nfc := "\ud55c"             // 한
	if nfd == nfc {
		t.Fatal("NFD 리터럴이 NFC 와 같다 — 테스트가 무의미하다")
	}
	if got := NFC(nfd); got != nfc {
		t.Errorf("NFC(NFD) = %q, want %q", got, nfc)
	}
	if got := NFC(nfc); got != nfc {
		t.Errorf("NFC 는 멱등이어야 한다")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"저장엔진 OPFS", "저장엔진-OPFS"}, // 저장엔진 OPFS -> 저장엔진-OPFS
		{"a/b:c*d?e", "a-b-c-d-e"},
		{"  앞뒤 공백  ", "앞뒤-공백"},   // 앞뒤 공백 -> 앞뒤-공백
		{"--앞뒤 하이픈--", "앞뒤-하이픈"}, // 앞뒤 하이픈 -> 앞뒤-하이픈
		{"연속   공백", "연속-공백"},     // 연속 공백 -> 연속-공백
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	long := Slugify(strings.Repeat("한", 140)) // 한 x 140
	if utf8.RuneCountInString(long) != 80 {
		t.Errorf("긴 slug 가 80 rune 으로 안 잘렸다: %d", utf8.RuneCountInString(long))
	}
	if !utf8.ValidString(long) {
		t.Error("긴 slug 가 깨진 UTF-8")
	}
}

// TestSlugifyAllBadChars 는 slugBad 상수에 든 모든 문자(/ \ : * ? " < > |)가
// 각각 하이픈으로 바뀌는지 확인한다. TestSlugify 는 "a/b:c*d?e" 하나만 보므로
// slugBad 에 문자를 추가·삭제해도 이 테스트 없이는 회귀를 못 잡는다.
func TestSlugifyAllBadChars(t *testing.T) {
	for _, r := range slugBad {
		in := "a" + string(r) + "b"
		want := "a-b"
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q (bad char %q)", in, got, want, r)
		}
	}
}
