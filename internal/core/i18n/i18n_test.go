package i18n

import "testing"

// 모르는 값·빈 값은 한국어다 — 기존 볼트가 전부 한국어라 그쪽이 안전한 기본값이다.
// 여기서 영어로 떨어지면 돌던 볼트의 색인 머리말이 어느 날 갑자기 바뀐다.
func TestUnknownFallsBackToKorean(t *testing.T) {
	for _, s := range []string{"", "ko", "KO", "ja", "쓰레기"} {
		if got := Of(s); got != KO {
			t.Errorf("Of(%q) = %q, KO 여야 한다", s, got)
		}
	}
	if got := Of("en"); got != EN {
		t.Errorf("Of(\"en\") = %q", got)
	}
}

func TestPicksByLang(t *testing.T) {
	if got := KO.T("가", "a"); got != "가" {
		t.Errorf("KO.T = %q", got)
	}
	if got := EN.T("가", "a"); got != "a" {
		t.Errorf("EN.T = %q", got)
	}
}
